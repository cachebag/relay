package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// `PostedResult` is one POST /jobs/{id}/result the fake scheduler received.
type PostedResult struct {
	jobID string
	body  map[string]any
}

// `FakeScheduler` is an httptest server speaking the node-facing contract:
// heartbeats, GET /nodes/{id}/jobs/next and POST /jobs/{id}/result. Tests set
// next and resultStatus to script its replies.
type FakeScheduler struct {
	srv *httptest.Server
	// `cacheDir` stands in for the user cache directory of every agent
	// built with `newTestAgent`, so agents in one test share it the way a
	// restarted agent would.
	cacheDir string

	heartbeats atomic.Int32
	nextCalls  atomic.Int32
	// `heartbeatSeen` wakes up `waitHeartbeats` without
	// any polling or sleeping.
	heartbeatSeen chan struct{}

	mu           sync.Mutex
	nodeIDs      []string
	next         func(w http.ResponseWriter)
	resultStatus int
	results      []PostedResult
	resultPosted chan struct{}
}

// Creates a new FakeScheduler and starts its HTTP server. The server
// will be closed automatically when the test ends.
func newFakeScheduler(t *testing.T) *FakeScheduler {
	t.Helper()
	// Initialize the FakeScheduler with default values.
	f := &FakeScheduler{
		cacheDir:      t.TempDir(),
		heartbeatSeen: make(chan struct{}, 1),
		resultPosted:  make(chan struct{}, 16),
		next:          func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) },
		resultStatus:  http.StatusNoContent,
	}

	mux := http.NewServeMux()

	// Register the HTTP handlers for the FakeScheduler.
	mux.HandleFunc("POST /nodes/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		f.heartbeats.Add(1)
		select {
		case f.heartbeatSeen <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /nodes/{id}/jobs/next", func(w http.ResponseWriter, r *http.Request) {
		f.nextCalls.Add(1)
		f.mu.Lock()
		f.nodeIDs = append(f.nodeIDs, r.PathValue("id"))
		next := f.next
		f.mu.Unlock()
		next(w)
	})
	mux.HandleFunc("POST /jobs/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding result body: %v", err)
		}
		f.mu.Lock()
		f.results = append(f.results, PostedResult{jobID: r.PathValue("id"), body: body})
		status := f.resultStatus
		f.mu.Unlock()
		w.WriteHeader(status)
		f.resultPosted <- struct{}{}
	})

	// Start the HTTP server for the FakeScheduler.
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// `setNext` sets the function that will be called when the FakeScheduler
// receives a GET /nodes/{id}/jobs/next request.
func (f *FakeScheduler) setNext(fn func(w http.ResponseWriter)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next = fn
}

// `setResultStatus` sets the HTTP status code that the FakeScheduler
// will return when it receives a POST /jobs/{id}/result request.
func (f *FakeScheduler) setResultStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resultStatus = status
}

// `posted` returns a copy of all results that have been posted to
// the FakeScheduler.
func (f *FakeScheduler) posted() []PostedResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PostedResult(nil), f.results...)
}

// `waitHeartbeats` blocks until the server has seen at least n heartbeats.
func (f *FakeScheduler) waitHeartbeats(n int32) {
	for f.heartbeats.Load() < n {
		<-f.heartbeatSeen
	}
}

// `hasPendingFile` reports whether an agent has a result saved on disk.
func (f *FakeScheduler) hasPendingFile() bool {
	_, err := os.Stat(filepath.Join(f.cacheDir, pendingFile))
	return err == nil
}

// `serveJob` replies 200 with the given job.
func serveJob(job store.Job) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}
}

// `FakeRunner` records the commands it is asked to run and returns res.
type FakeRunner struct {
	mu    sync.Mutex
	calls [][]string
	res   Result
}

// `run` records the command it is asked to run and returns
// the pre-set result.
func (r *FakeRunner) run(ctx context.Context, argv []string, dir string) Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, argv)
	return r.res
}

// `count` returns the number of times `run` has been called.
func (r *FakeRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// `newTestAgent` creates a new Agent for testing, using
// the given FakeScheduler, node ID, and Runner.
func newTestAgent(f *FakeScheduler, id string, run Runner) *Agent {
	a := newAgent(f.srv.URL, "test-token", store.Node{ID: id, Hostname: "test"}, f.cacheDir)
	a.client = f.srv.Client()
	a.run = run
	return a
}

// `tickAndWait` runs one interval and waits for any job it started to finish.
func tickAndWait(a *Agent) {
	a.Tick(context.Background())
	a.wg.Wait()
}

// `TestTickNoJob` verifies that ticking the agent with no available job
// results in a single heartbeat and next call, but no
// runner invocations or posted results.
func TestTickNoJob(t *testing.T) {
	f := newFakeScheduler(t)
	runner := &FakeRunner{}
	a := newTestAgent(f, "node-1", runner.run)

	tickAndWait(a)

	if got := f.heartbeats.Load(); got != 1 {
		t.Errorf("heartbeats = %d, want 1", got)
	}
	if got := f.nextCalls.Load(); got != 1 {
		t.Errorf("jobs/next calls = %d, want 1", got)
	}
	if got := runner.count(); got != 0 {
		t.Errorf("runner called %d times, want 0", got)
	}
	if got := f.posted(); len(got) != 0 {
		t.Errorf("posted %d results, want 0", len(got))
	}
}

// `TestTickRunsAndReportsJob` verifies that ticking the agent with an available job
// results in the runner being invoked and the result being posted.
func TestTickRunsAndReportsJob(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{
		ID:       "job-1",
		Command:  []string{"echo", "hi"},
		Status:   store.JobRunning,
		Attempts: 2,
	}))
	runner := &FakeRunner{res: Result{ExitCode: 3, Stdout: "out\n", Stderr: "err\n"}}
	a := newTestAgent(f, "node-1", runner.run)

	tickAndWait(a)

	if runner.count() != 1 {
		t.Fatalf("runner called %d times, want 1", runner.count())
	}
	if got := runner.calls[0]; len(got) != 2 || got[0] != "echo" || got[1] != "hi" {
		t.Errorf("runner argv = %v, want [echo hi]", got)
	}

	// Verify that the result posted matches the expected values.
	results := f.posted()
	if len(results) != 1 {
		t.Fatalf("posted %d results, want 1", len(results))
	}
	got := results[0]
	if got.jobID != "job-1" {
		t.Errorf("result posted for job %q, want job-1", got.jobID)
	}
	want := map[string]any{
		"exit_code": float64(3),
		"stdout":    "out\n",
		"stderr":    "err\n",
		"node_id":   "node-1",
		"attempt":   float64(2),
	}
	for k, v := range want {
		if got.body[k] != v {
			t.Errorf("result %s = %#v, want %#v", k, got.body[k], v)
		}
	}
}

// `TestReportSendsZeroExitCode` verifies that a zero exit code is explicitly sent in the result.
// A zero exit code must be sent explicitly: the server decodes exit_code into
// a pointer and treats a missing field as a bad request.
func TestReportSendsZeroExitCode(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}}))
	a := newTestAgent(f, "node-1", (&FakeRunner{}).run)

	tickAndWait(a)

	results := f.posted()
	if len(results) != 1 {
		t.Fatalf("posted %d results, want 1", len(results))
	}
	for _, k := range []string{"exit_code", "stdout", "stderr", "node_id", "attempt"} {
		if _, ok := results[0].body[k]; !ok {
			t.Errorf("result body is missing %q", k)
		}
	}
	if results[0].body["exit_code"] != float64(0) {
		t.Errorf("exit_code = %v, want 0", results[0].body["exit_code"])
	}
}

// `TestTickServerError` verifies that the agent handles a server error gracefully,
// does not run the job, and retries on the next tick.
func TestTickServerError(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) })
	runner := &FakeRunner{}
	a := newTestAgent(f, "node-1", runner.run)

	tickAndWait(a)

	if runner.count() != 0 {
		t.Errorf("runner called %d times after a 500, want 0", runner.count())
	}
	if got := f.posted(); len(got) != 0 {
		t.Errorf("posted %d results after a 500, want 0", len(got))
	}

	// The next interval works normally once the scheduler recovers.
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}}))
	tickAndWait(a)

	if runner.count() != 1 {
		t.Errorf("runner called %d times after recovery, want 1", runner.count())
	}
	if got := f.posted(); len(got) != 1 {
		t.Errorf("posted %d results after recovery, want 1", len(got))
	}
	if got := f.heartbeats.Load(); got != 2 {
		t.Errorf("heartbeats = %d, want 2", got)
	}
}

// `TestTickStaleResultIsDropped` verifies that when the server responds with a 409,
// indicating the job was reassigned while this node ran it, the result is
// dropped, and the agent is free to take new work.
func TestTickStaleResultIsDropped(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}, Attempts: 1}))
	f.setResultStatus(http.StatusConflict)
	runner := &FakeRunner{}
	a := newTestAgent(f, "node-1", runner.run)

	tickAndWait(a)

	if got := f.posted(); len(got) != 1 {
		t.Fatalf("posted %d results, want exactly 1 (a 409 is not retried)", len(got))
	}
	if a.busy.Load() {
		t.Error("agent still busy after a 409")
	}
	if f.hasPendingFile() {
		t.Error("pending file still on disk after a 409")
	}

	// The scheduler has moved on, so the agent keeps polling as usual.
	f.setNext(serveJob(store.Job{ID: "job-2", Command: []string{"true"}, Attempts: 1}))
	f.setResultStatus(http.StatusNoContent)
	tickAndWait(a)

	results := f.posted()
	if len(results) != 2 || results[1].jobID != "job-2" {
		t.Errorf("posted %+v, want a second result for job-2", results)
	}
}

// `TestTickEscapesNodeID` verifies that the agent correctly escapes node IDs
// when communicating with the server.
func TestTickEscapesNodeID(t *testing.T) {
	f := newFakeScheduler(t)
	a := newTestAgent(f, "rack 1/node#2", (&FakeRunner{}).run)

	tickAndWait(a)

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.nodeIDs) != 1 || f.nodeIDs[0] != "rack 1/node#2" {
		t.Errorf("server saw node IDs %q, want [%q]", f.nodeIDs, "rack 1/node#2")
	}
}

// `TestHeartbeatsContinueWhileJobRuns` verifies that while a job is
// running the agent must keep heartbeating,or the scheduler marks
// the node offline and requeues the job, but must not ask for more work.
func TestHeartbeatsContinueWhileJobRuns(t *testing.T) {
	f := newFakeScheduler(t)
	// Behaves like the real server: a running job is handed back on every poll
	// until its result arrives.
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"sleep", "45"}}))

	// Set up a job that blocks until explicitly released, allowing us to test
	// heartbeats while the job is running.
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int32
	blocked := func(ctx context.Context, argv []string, dir string) Result {
		runs.Add(1)
		close(started)
		<-release
		return Result{}
	}
	a := newTestAgent(f, "node-1", blocked)

	// Start the agent in a separate goroutine to allow it to run concurrently with the test.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx, time.Millisecond) }()

	// Wait for the job to start running before checking heartbeats.
	<-started
	nextCallsWhenStarted := f.nextCalls.Load()
	f.waitHeartbeats(f.heartbeats.Load() + 2)

	// Verify that the agent did not ask for new work while the job was running.
	if got := f.nextCalls.Load(); got != nextCallsWhenStarted {
		t.Errorf("asked for new work while a job was running: %d calls, want %d",
			got, nextCallsWhenStarted)
	}
	if len(f.posted()) != 0 {
		t.Error("result posted before the job finished")
	}

	// Release the job and wait for the result to be posted.
	close(release)
	<-f.resultPosted

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run returned %v, want context.Canceled", err)
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("job ran %d times, want 1", got)
	}
	if got := f.posted(); len(got) != 1 {
		t.Errorf("posted %d results, want 1", len(got))
	}
}

// `TestInterruptedJobIsNotReported` verifies that a job killed by agent shutdown
// is not considered a failure, so no result is reported and the scheduler hands it out again.
func TestInterruptedJobIsNotReported(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"sleep", "45"}}))

	// Set up a job that blocks until the context is canceled, allowing us to test
	// that interrupted jobs are not reported.
	started := make(chan struct{})
	waitForCancel := func(ctx context.Context, argv []string, dir string) Result {
		close(started)
		<-ctx.Done()
		return Result{ExitCode: -1, Stderr: ctx.Err().Error()}
	}
	a := newTestAgent(f, "node-1", waitForCancel)

	// Start the agent and wait for the job to start.
	ctx, cancel := context.WithCancel(context.Background())
	a.Tick(ctx)
	<-started
	// Cancel the context to simulate agent shutdown and wait for the agent to finish.
	cancel()
	a.wg.Wait()

	// Verify that no result was posted for the interrupted job.
	if got := f.posted(); len(got) != 0 {
		t.Errorf("posted %d results for an interrupted job, want 0", len(got))
	}
	if f.hasPendingFile() {
		t.Error("interrupted job left a pending result on disk")
	}
}
