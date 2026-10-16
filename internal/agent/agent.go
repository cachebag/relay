package agent

import (
	"context"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Runner executes a job's command and reports how it went. It is a field on
// Agent so tests can replace it with a fake.
type Runner func(ctx context.Context, argv []string) Result

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

	// cacheDir is where an unreported result is saved so it survives a
	// restart. Empty means keep it in memory only. A field so tests can
	// point it at a temp directory.
	cacheDir string
	// pending is a finished result the scheduler hasn't acknowledged yet.
	// It is only touched by the job goroutine while busy, and by Tick
	// otherwise.
	pending *pending

	// busy is true from the moment a job is picked up until it has finished
	// and its result has been reported once.
	busy atomic.Bool
	wg   sync.WaitGroup
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
	n.ID = loadOrCreateNodeID(cacheDir, n.ID)
	a := &Agent{
		schedulerURL: schedulerURL,
		poolToken:    poolToken,
		node:         n,
		client:       &http.Client{Timeout: 5 * time.Second},
		run:          RunCommand,
		cacheDir:     cacheDir,
	}
	a.loadPending()
	return a
}

// Tick does one interval of work: send a heartbeat, then, if no job is
// already running, report any result still waiting to be sent, then ask the
// scheduler for the next job and start it in the background. While a result
// can't be reported, no new job is fetched. It returns without waiting for the job to finish, so the caller
// keeps heartbeating while the job runs. Failures are logged and retried on
// the next Tick, since the scheduler being unreachable is a normal condition.
//
// Kept separate from Run so tests can call it directly.
func (a *Agent) Tick(ctx context.Context) {
	if err := Send(ctx, a.client, a.schedulerURL, a.node, a.poolToken); err != nil {
		log.Printf("heartbeat failed: %v", err)
	} else {
		log.Printf("heartbeat sent to %s", a.schedulerURL)
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
	if err != nil {
		log.Printf("fetching next job failed: %v", err)
		return
	}
	if job == nil {
		return
	}

	log.Printf("running job %s: %v", job.ID, job.Command)
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
	res := a.run(ctx, job.Command)

	// A job killed because the agent is shutting down didn't really fail, so
	// leave it unreported for the scheduler to hand out again.
	if ctx.Err() != nil {
		log.Printf("job %s interrupted, not reporting a result", job.ID)
		return
	}

	a.setPending(pending{JobID: job.ID, Attempt: job.Attempts, Result: res})
	a.flushPending(ctx)
}

// Run calls Tick every interval until ctx is cancelled, then waits for any
// running job to wind down and returns ctx.Err().
func (a *Agent) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer a.wg.Wait()

	for {
		a.Tick(ctx)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
