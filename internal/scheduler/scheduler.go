package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/cachebag/relay/internal/store"
)

const nodeTimeout = 30 * time.Second

type Scheduler struct {
	nodes store.Registry
	jobs  store.JobStore
}

func New(nodes store.Registry, jobs store.JobStore) *Scheduler {
	return &Scheduler{nodes: nodes, jobs: jobs}
}

// Tick does one round of assignment. It is separate from Run so tests can call it directly.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return err
	}
	online := nodes[:0]
	for _, n := range nodes {
		if now.Sub(n.LastSeen) < nodeTimeout {
			online = append(online, n)
		}
	}
	if len(online) == 0 {
		return nil
	}

	jobs, err := s.jobs.List(ctx)
	if err != nil {
		return err
	}
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

// Run calls Tick on an interval until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
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
