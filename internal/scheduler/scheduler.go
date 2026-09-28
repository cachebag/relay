package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// maxAttempts caps how many times a job is placed on a node. A job that has
// been handed out this many times and still lost its node is marked failed
// instead of being requeued, so a job that keeps taking nodes down with it
// can't loop through the cluster forever.
const maxAttempts = 3

type Scheduler struct {
	nodes store.Registry
	jobs  store.JobStore
}

func New(nodes store.Registry, jobs store.JobStore) *Scheduler {
	return &Scheduler{nodes: nodes, jobs: jobs}
}

// Does one round of scheduling.
// First hands back any work sitting on a node that stopped heartbeating, then
// assigns queued jobs to online nodes and updates their status and NodeID.
// Kept separate from Run so tests can call it directly.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {

	// Collects and stores list of nodes.
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return err
	}

	// Requeue before placement so a job lost this tick can be handed to a
	// different node in the same tick rather than waiting for the next one.
	if err := s.requeueLost(ctx, now, nodes); err != nil {
		return err
	}

	// Filters out offline nodes based on their LastSeen timestamp.
	online := make([]store.Node, 0, len(nodes))
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
		j.Attempts++
		i++
		if err := s.jobs.Update(ctx, j); err != nil {
			return err
		}
		log.Printf("assigned job %s to node %s (attempt %d)", j.ID, j.NodeID, j.Attempts)
	}
	return nil
}

// requeueLost puts jobs back in the queue when the node they were placed on
// has gone offline, or fails them once they have used up maxAttempts.
//
// A job whose NodeID is not in nodes at all counts as lost too: the node was
// never registered (or the registry was wiped), so nothing is going to run it.
func (s *Scheduler) requeueLost(ctx context.Context, now time.Time, nodes []store.Node) error {
	online := map[string]bool{}
	for _, n := range nodes {
		if store.Online(n, now) {
			online[n.ID] = true
		}
	}

	jobs, err := s.jobs.List(ctx)
	if err != nil {
		return err
	}

	for _, j := range jobs {
		// Only work that is currently sitting on a node can be lost.
		if j.Status != store.JobAssigned && j.Status != store.JobRunning {
			continue
		}
		if online[j.NodeID] {
			continue
		}
		lost := j.NodeID
		if j.Attempts >= maxAttempts {
			j.Status = store.JobFailed
		} else {
			j.Status = store.JobQueued
		}
		j.NodeID = ""
		if err := s.jobs.Update(ctx, j); err != nil {
			return err
		}
		log.Printf("node %s offline: job %s -> %s (attempt %d)", lost, j.ID, j.Status, j.Attempts)
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
