package agent

import (
	"context"
	"errors"
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

	// busy is true from the moment a job is picked up until its result has
	// been reported (or given up on).
	busy atomic.Bool
	wg   sync.WaitGroup
}

// New returns an Agent for node n that talks to the scheduler at schedulerURL
// and runs commands on the local machine.
func New(schedulerURL, poolToken string, n store.Node) *Agent {
	return &Agent{
		schedulerURL: schedulerURL,
		poolToken:    poolToken,
		node:         n,
		client:       &http.Client{Timeout: 5 * time.Second},
		run:          RunCommand,
	}
}

// Tick does one interval of work: send a heartbeat, then, if no job is
// already running, ask the scheduler for the next one and start it in the
// background. It returns without waiting for the job to finish, so the caller
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

// execute runs one job and reports its result. A failed report is only
// logged: the scheduler still has the job as running and will hand it back on
// a later poll. A 409 means the job now belongs to another node, so the
// result is dropped.
func (a *Agent) execute(ctx context.Context, job store.Job) {
	res := a.run(ctx, job.Command)

	// A job killed because the agent is shutting down didn't really fail, so
	// leave it unreported for the scheduler to hand out again.
	if ctx.Err() != nil {
		log.Printf("job %s interrupted, not reporting a result", job.ID)
		return
	}

	switch err := a.Report(ctx, job, resultBody{
		NodeID:  a.node.ID,
		Attempt: job.Attempts,
		Result:  res,
	}); {
	case errors.Is(err, errStale):
		log.Printf("result for job %s discarded: reassigned to another node", job.ID)
		return
	case err != nil:
		log.Printf("reporting job %s failed: %v", job.ID, err)
		return
	}
	log.Printf("job %s finished with exit code %d", job.ID, res.ExitCode)
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
