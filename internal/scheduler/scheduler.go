package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/cachebag/relay/internal/store"
)

type Scheduler struct {
	nodes store.Registry
	jobs  store.JobStore
}

func New(nodes store.Registry, jobs store.JobStore) *Scheduler {
	return &Scheduler{nodes: nodes, jobs: jobs}
}

// Does one round of job assignment for scheduler.
// Assigns queued jobs to online nodes and updates their status and NodeID accordingly.
// Kept separate from Run so tests can call it directly.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {

	// Collects and stores list of online nodes.
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return err
	}

	// Filters out offline nodes based on their LastSeen timestamp.
	online := nodes[:0]
	for _, n := range nodes {
		if store.Online(n, now) {
			online = append(online, n)
		}
	}

	// Checks to see if there are any online nodes. If not, return early.
	if len(online) == 0 {
		return nil
	}

	// Collects and stores list of jobs.
	jobs, err := s.jobs.List(ctx)
	if err != nil {
		return err
	}

	// Assigns queued jobs round-robin across online nodes, oldest job first.
	i := 0
	for _, j := range jobs {
		if j.Status != store.JobQueued {
			continue
		}
		j.Status = store.JobAssigned
		j.NodeID = online[i%len(online)].ID
		i++
		if err := s.jobs.Update(ctx, j); err != nil {
			return err
		}
		log.Printf("assigned job %s to node %s", j.ID, j.NodeID)
	}
	return nil
}

// Run continuously attempts to assign queued jobs to online nodes at the specified interval.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)

	// Stops the ticker once the function stops running.
	defer t.Stop()

	// Main loop for job assignment.
	for {
		select {
		case <-t.C:
			if err := s.Tick(ctx, time.Now()); err != nil {
				log.Printf("scheduler tick: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
