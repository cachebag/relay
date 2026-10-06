package agent

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// `noJob` replies 204: nothing to do.
func noJob(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// `TestPendingRetriedUntilAccepted` verifies that a result the scheduler
// fails to take (500) is kept on disk and resent every interval, and that no
// new job is fetched until it goes through.
func TestPendingRetriedUntilAccepted(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}, Attempts: 1}))
	f.setResultStatus(http.StatusInternalServerError)
	runner := &FakeRunner{res: Result{ExitCode: 0, Stdout: "ok\n"}}
	a := newTestAgent(f, "node-1", runner.run)

	// Interval 1 runs the job and gets a 500 on the first report.
	tickAndWait(a)
	f.setNext(noJob)
	if !f.hasPendingFile() {
		t.Fatal("no pending file after a failed report")
	}

	// Interval 2 gets another 500 and must not ask for more work.
	tickAndWait(a)
	if got := len(f.posted()); got != 2 {
		t.Fatalf("posted %d times after two intervals, want 2", got)
	}
	if got := f.nextCalls.Load(); got != 1 {
		t.Errorf("jobs/next called %d times while a result was pending, want 1", got)
	}

	// Interval 3 succeeds, clears the result and goes back to fetching work.
	f.setResultStatus(http.StatusNoContent)
	tickAndWait(a)

	results := f.posted()
	if len(results) != 3 {
		t.Fatalf("posted %d times, want 3", len(results))
	}
	if results[2].jobID != "job-1" || results[2].body["stdout"] != "ok\n" {
		t.Errorf("third post = %+v, want job-1's result", results[2])
	}
	if got := f.nextCalls.Load(); got != 2 {
		t.Errorf("jobs/next called %d times, want 2 (once more after the result went through)", got)
	}
	if runner.count() != 1 {
		t.Errorf("runner called %d times, want 1 (retrying must not rerun the job)", runner.count())
	}
	if f.hasPendingFile() {
		t.Error("pending file still on disk after the result was accepted")
	}
}

// `TestPendingDroppedOnRejection` verifies that a 4xx other than 409 (here,
// the job no longer exists) also drops the result, since resending it will
// never work and would block the node forever.
func TestPendingDroppedOnRejection(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}, Attempts: 1}))
	f.setResultStatus(http.StatusNotFound)
	a := newTestAgent(f, "node-1", (&FakeRunner{}).run)

	tickAndWait(a)
	f.setNext(noJob)
	tickAndWait(a)

	if got := len(f.posted()); got != 1 {
		t.Errorf("posted %d times, want 1 (a 404 is not retried)", got)
	}
	if got := f.nextCalls.Load(); got != 2 {
		t.Errorf("jobs/next called %d times, want 2", got)
	}
	if f.hasPendingFile() {
		t.Error("pending file still on disk after a 404")
	}
}

// `TestPendingSentOnStartup` verifies that a result left on disk by a
// previous run is loaded by a fresh agent and sent before it asks for work.
func TestPendingSentOnStartup(t *testing.T) {
	f := newFakeScheduler(t)
	data, err := json.Marshal(pending{
		JobID:   "job-1",
		Attempt: 2,
		Result:  Result{ExitCode: 7, Stdout: "out\n", Stderr: "err\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cacheDir, pendingFile), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// The result must already be posted by the time the agent asks for work.
	f.setNext(func(w http.ResponseWriter) {
		if len(f.posted()) != 1 {
			t.Error("asked for work before sending the pending result")
		}
		noJob(w)
	})
	runner := &FakeRunner{}
	a := newTestAgent(f, "node-1", runner.run)
	tickAndWait(a)

	results := f.posted()
	if len(results) != 1 {
		t.Fatalf("posted %d results, want 1", len(results))
	}
	if results[0].jobID != "job-1" {
		t.Errorf("result posted for job %q, want job-1", results[0].jobID)
	}
	want := map[string]any{
		"exit_code": float64(7),
		"stdout":    "out\n",
		"stderr":    "err\n",
		"node_id":   "node-1",
		"attempt":   float64(2),
	}
	for k, v := range want {
		if results[0].body[k] != v {
			t.Errorf("result %s = %#v, want %#v", k, results[0].body[k], v)
		}
	}
	if runner.count() != 0 {
		t.Errorf("runner called %d times, want 0", runner.count())
	}
	if f.hasPendingFile() {
		t.Error("pending file still on disk after the result was accepted")
	}
}

// `TestPendingSurvivesRestart` verifies the whole round trip: an agent that
// can't report its result is replaced by a new one, which reports it.
func TestPendingSurvivesRestart(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}, Attempts: 1}))
	f.setResultStatus(http.StatusServiceUnavailable)
	tickAndWait(newTestAgent(f, "node-1", (&FakeRunner{res: Result{ExitCode: 1}}).run))

	f.setNext(noJob)
	f.setResultStatus(http.StatusNoContent)
	tickAndWait(newTestAgent(f, "node-1", (&FakeRunner{}).run))

	results := f.posted()
	if len(results) != 2 || results[1].jobID != "job-1" || results[1].body["exit_code"] != float64(1) {
		t.Errorf("posted %+v, want job-1's result (exit code 1) resent by the new agent", results)
	}
	if f.hasPendingFile() {
		t.Error("pending file still on disk after the result was accepted")
	}
}

// `TestUnreadablePendingFileIsDiscarded` verifies that a corrupt pending file
// is deleted instead of blocking the agent.
func TestUnreadablePendingFileIsDiscarded(t *testing.T) {
	f := newFakeScheduler(t)
	if err := os.WriteFile(filepath.Join(f.cacheDir, pendingFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newTestAgent(f, "node-1", (&FakeRunner{}).run)
	tickAndWait(a)

	if f.hasPendingFile() {
		t.Error("corrupt pending file was not removed")
	}
	if got := len(f.posted()); got != 0 {
		t.Errorf("posted %d results, want 0", got)
	}
	if got := f.nextCalls.Load(); got != 1 {
		t.Errorf("jobs/next called %d times, want 1", got)
	}
}

// `TestNoCacheDirKeepsPendingInMemory` verifies that without a cache
// directory the result is still retried, just not saved to disk.
func TestNoCacheDirKeepsPendingInMemory(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}, Attempts: 1}))
	f.setResultStatus(http.StatusInternalServerError)
	a := newTestAgent(f, "node-1", (&FakeRunner{}).run)
	a.cacheDir = ""

	tickAndWait(a)
	f.setNext(noJob)
	f.setResultStatus(http.StatusNoContent)
	tickAndWait(a)

	if got := len(f.posted()); got != 2 {
		t.Errorf("posted %d times, want 2", got)
	}
	if f.hasPendingFile() {
		t.Error("wrote a pending file with no cache directory")
	}
}
