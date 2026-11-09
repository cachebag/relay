package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Runner executes a job's command in dir and reports how it went. It is a
// field on Agent so tests can replace it with a fake.
type Runner func(ctx context.Context, argv []string, dir string) Result

// Agent is one node's side of the scheduler conversation: it heartbeats, asks
// for its next job, runs it and reports the result.
// A job runs in its own goroutine so the heartbeat loop never blocks on it.
// Tick and Run must be called from a single goroutine.
type Agent struct {
	schedulerURL string
	poolToken    string
	node         store.Node
	client       *http.Client
	run          Runner
	// connected tracks whether the most recent heartbeat successfully reached
	// the scheduler. This prevents the agent from printing the connected
	// message after every successful heartbeat.
	connected bool

	// retryInterval stores how long the agent waits between heartbeat attempts.
	// It is used when displaying a connection failure to the user.
	retryInterval time.Duration

	// cacheDir is where an unreported result is saved so it survives a
	// restart. Empty means keep it in memory only. A field so tests can
	// point it at a temp directory.
	cacheDir string

	// keepWorkdir leaves each job's folder in place after its result is
	// reported, for debugging.
	keepWorkdir bool

	// pending is a finished result the scheduler hasn't acknowledged yet.
	// It is only touched by the job goroutine while busy, and by Tick
	// otherwise.
	pending *pending

	// busy is true from the moment a job is picked up until it has finished
	// and its result has been reported once.
	busy atomic.Bool
	wg   sync.WaitGroup

	// removed is set by Tick when the scheduler says a pool admin removed
	// this machine. Run then stops instead of retrying forever.
	removed       bool
	rotatedWarned bool
}

// New returns an Agent for node n that talks to the scheduler at schedulerURL
// and runs commands on the local machine. A result left unreported by a
// previous run is loaded from the user cache directory and sent first.
func New(schedulerURL, poolToken string, n store.Node) *Agent {
	return newAgent(schedulerURL, poolToken, n, defaultCacheDir())
}

// newAgent is New with the cache directory chosen by the caller, so tests
// can use a temp directory.
func newAgent(schedulerURL, poolToken string, n store.Node, cacheDir string) *Agent {
	// Chat apps turn the address into a link and add a trailing slash. The
	// paths below start with one, and "//nodes/heartbeat" is redirected by
	// the scheduler to a GET, which it then refuses. Strip it here once.
	schedulerURL = strings.TrimRight(schedulerURL, "/")
	n.ID = loadOrCreateNodeID(cacheDir, n.ID)
	a := &Agent{
		schedulerURL: schedulerURL,
		poolToken:    poolToken,
		node:         n,
		client:       &http.Client{Timeout: 5 * time.Second},
		run:          RunCommand,

		// The relay agent normally heartbeats every 10 seconds.
		// Run replaces this value with the interval it receives, which
		// allows tests to use shorter intervals.
		retryInterval: 10 * time.Second,

		cacheDir: cacheDir,
	}
	a.loadPending()
	return a
}

// KeepWorkdir leaves job folders in place instead of deleting them, and
// stops Run from clearing leftover ones when it starts.
func (a *Agent) KeepWorkdir() {
	a.keepWorkdir = true
}

// isConnectionError reports whether err represents a networking failure.
// HTTP response errors such as a rejected pool token are handled separately.
func isConnectionError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

// Tick does one interval of work: send a heartbeat, then, if no job is
// already running, report any result still waiting to be sent, then ask the
// scheduler for the next job and start it in the background. While a result
// can't be reported, no new job is fetched. It returns without waiting for
// the job to finish, so the caller keeps heartbeating while the job runs.
// Failures are logged and retried on the next Tick, since the scheduler being
// unreachable is a normal condition.
//
// Kept separate from Run so tests can call it directly.
func (a *Agent) Tick(ctx context.Context) {
	// Send the node heartbeat to the scheduler.
	err := Send(ctx, a.client, a.schedulerURL, a.node, a.poolToken)
	if err != nil {
		// Any failed heartbeat means the agent is no longer considered
		// connected. The next successful heartbeat will print the
		// connected message again.
		a.connected = false

		// If the context was cancelled because the agent is shutting down,
		// return without printing a misleading connection failure.
		if ctx.Err() != nil {
			return
		}

		// Give the user a more helpful message depending on why the
		// heartbeat failed.
		switch {
		case errors.Is(err, ErrRemoved):
			log.Printf("removed from this pool by the admin")
			a.removed = true

		case errors.Is(err, ErrTokenRotated):
			if !a.rotatedWarned {
				log.Printf("%s", store.RotatedTokenMsg)
				a.rotatedWarned = true
			}

		case errors.Is(err, ErrBadToken):
			log.Printf(
				"pool token rejected; check it with your pool admin",
			)

		case isConnectionError(err):
			log.Printf(
				"can't reach scheduler at %s; retrying in %s",
				a.schedulerURL,
				a.retryInterval,
			)

		case errors.Is(err, ErrPoolConflict):
			log.Printf("node ID already belongs to another pool; leave that pool before joining this one")

		default:
			// Keep a general fallback for scheduler errors that are
			// neither a bad token nor a networking failure.
			log.Printf("heartbeat failed: %v", err)
		}

		// Do not make additional scheduler requests during this Tick.
		// The agent will try again on the next heartbeat interval.
		return
	}

	// Only print the connected message on the first successful heartbeat
	// or after recovering from a previous heartbeat failure.
	if !a.connected {
		log.Printf("connected; this machine is online in the pool")
		a.connected = true
		a.rotatedWarned = false
	}

	// The scheduler hands a running job back to its node on every poll, so
	// asking while one is in flight would just run it twice.
	if a.busy.Load() {
		return
	}

	if !a.flushPending(ctx) {
		return
	}

	job, err := a.Next(ctx)
	if errors.Is(err, ErrBadToken) {
		log.Printf("pool token rejected; check it with your pool admin")
		return
	}
	if err != nil {
		log.Printf("fetching next job failed: %v", err)
		return
	}
	if job == nil {
		return
	}

	if job.BundleBytes > 0 {
		log.Printf("running job %s: %s %v (%d bytes of files)", job.ID, entryName(job.Entry), job.Command, job.BundleBytes)
	} else {
		log.Printf("running job %s: %v", job.ID, job.Command)
	}
	a.busy.Store(true)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.busy.Store(false)
		a.execute(ctx, *job)
	}()
}

// execute runs one job, saves its result as pending and tries to report it
// straight away. If that fails, Tick keeps retrying it before fetching more
// work.
func (a *Agent) execute(ctx context.Context, job store.Job) {
	// Legacy jobs have no timeout field. Cap unexpected values from older
	// schedulers as well, so every attempt has a bounded execution time.
	timeoutSeconds := job.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = store.DefaultJobTimeoutSeconds
	}
	if timeoutSeconds > store.MaxJobTimeoutSeconds {
		timeoutSeconds = store.MaxJobTimeoutSeconds
	}

	// Create an independent deadline for this execution attempt.
	jobCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	var res Result
	dir, err := a.makeWorkdir(job.ID)
	if err != nil {
		res = Result{ExitCode: -1, Stderr: fmt.Sprintf("relay: can't create job folder: %v\n", err)}
	} else {
		res = a.runInWorkdir(jobCtx, job, dir)
	}

	// A job killed because the agent is shutting down didn't really fail, so
	// leave it unreported for the scheduler to hand out again. The same goes
	// for a bundle that couldn't be fetched: the scheduler hands the job back
	// on the next poll and the download is tried again.
	if ctx.Err() != nil || errors.Is(res.err, errBundleRetry) {
		if ctx.Err() != nil {
			log.Printf("job %s interrupted, not reporting a result", job.ID)
		} else {
			log.Printf("job %s: %v, will retry", job.ID, res.err)
		}
		a.removeWorkdir(job.ID)
		return
	}

	// Whatever the job left in out/ goes back with the result.
	outputPath := ""
	if dir != "" {
		switch path, size, err := packOutput(dir); {
		case err != nil:
			res.Stderr += fmt.Sprintf("relay: can't pack out/: %v\n", err)
		case size > maxOutputBundleBytes:
			res.Stderr += fmt.Sprintf("relay: out/ is %d MB, over the %d MB limit, files not returned\n", size>>20, maxOutputBundleBytes>>20)
			os.Remove(path)
		case path != "":
			outputPath = path
		}
	}

	// Report an execution deadline as a failed job with exit code -1.
	if errors.Is(jobCtx.Err(), context.DeadlineExceeded) {
		res.ExitCode = -1
		if res.Stderr != "" && !strings.HasSuffix(res.Stderr, "\n") {
			res.Stderr += "\n"
		}
		res.Stderr += fmt.Sprintf("relay: job timed out after %s\n", formatJobTimeout(timeoutSeconds))
	}

	a.setPending(pending{
		JobID:      job.ID,
		Attempt:    job.Attempts,
		Result:     res,
		OutputPath: outputPath,
	})
	a.flushPending(ctx)
}

// runInWorkdir fetches the job's files into dir if it has any, works out
// what to run, and runs it. Setup failures come back as a failed result
// with the reason on stderr, except a download worth retrying, which is
// returned in Result.err for execute to leave unreported.
func (a *Agent) runInWorkdir(ctx context.Context, job store.Job, dir string) Result {
	argv := job.Command
	if job.BundleBytes > 0 {
		if err := a.fetchBundle(ctx, job, dir); err != nil {
			if errors.Is(err, errBundleRetry) {
				return Result{ExitCode: -1, err: err}
			}
			return Result{ExitCode: -1, Stderr: fmt.Sprintf("relay: can't get job files: %v\n", err)}
		}
		var err error
		if argv, err = entryArgv(job.Entry, dir, job.Command, runtime.GOOS); err != nil {
			return Result{ExitCode: -1, Stderr: "relay: " + err.Error() + "\n"}
		}
	}
	// Every job gets an out/ folder to write results into.
	if err := os.MkdirAll(filepath.Join(dir, outputDir), 0o755); err != nil {
		return Result{ExitCode: -1, Stderr: fmt.Sprintf("relay: can't create out/: %v\n", err)}
	}
	return a.run(ctx, argv, dir)
}

// entryName is what the log calls the entry: the name, or the default.
func entryName(entry string) string {
	if entry == "" {
		return "run.sh/run.ps1/run.cmd"
	}
	return entry
}

// formatJobTimeout presents whole-second limits without redundant zeros.
func formatJobTimeout(seconds int) string {
	var formatted strings.Builder
	if hours := seconds / 3600; hours > 0 {
		fmt.Fprintf(&formatted, "%dh", hours)
	}
	if minutes := seconds % 3600 / 60; minutes > 0 {
		fmt.Fprintf(&formatted, "%dm", minutes)
	}
	if remainder := seconds % 60; remainder > 0 {
		fmt.Fprintf(&formatted, "%ds", remainder)
	}
	return formatted.String()
}

// Run calls Tick every interval until ctx is cancelled, then waits for any
// running job to wind down and returns ctx.Err().
func (a *Agent) Run(ctx context.Context, interval time.Duration) error {
	// Save the interval so connection failure messages can tell the user
	// when the next retry will occur.
	a.retryInterval = interval
	a.cleanWorkdirs()

	// stopJob kills a running job if this machine gets removed from the
	// pool. The scheduler has already put that job back in the queue.
	ctx, stopJob := context.WithCancel(ctx)
	defer stopJob()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer a.wg.Wait()

	for {
		a.Tick(ctx)
		// Removed by the pool admin: stop the job and return nil, so
		// relay-agent exits 0 instead of retrying a heartbeat that will
		// never be accepted. The deferred wg.Wait lets the job wind down.
		if a.removed {
			stopJob()
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
