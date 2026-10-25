package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/cachebag/relay/internal/credit"
	"github.com/cachebag/relay/internal/store"
)

// maxAttempts caps how many times a job is placed on a node.
// A job that keeps losing nodes cannot retry forever.
const maxAttempts = 3

type Scheduler struct {
	nodes   store.Registry
	jobs    store.JobStore
	credits *credit.Earner
	started time.Time
}

// New returns a Scheduler. credits pays owners for their machines' online
// time each tick; nil turns that off.
func New(nodes store.Registry, jobs store.JobStore, credits *credit.Earner) *Scheduler {
	return &Scheduler{nodes: nodes, jobs: jobs, credits: credits, started: time.Now()}
}

// Checks whether a given node satisfies a given job's OS requirement.
func validNode(n store.Node, j store.Job) bool {
	if j.Requires.OS != "" && n.OS != j.Requires.OS {
		return false
	}
	return true
}

// Tick performs one round of scheduling.
// It first recovers work from offline nodes, then assigns
// queued jobs to online nodes belonging to the same pool.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {

	// Get all registered nodes.
	nodes, err := s.nodes.List(ctx)
	if err != nil {
		return err
	}

	// Recover jobs from offline nodes before assigning new work.
	// Recovered jobs may be assigned again during this tick.
	if err := s.requeueLost(ctx, now, nodes); err != nil {
		return err
	}

	// Group online nodes by their pool.
	// Nodes without a pool cannot receive jobs.
	// online keeps the same nodes as a flat list for the credit ledger.
	onlineByPool := make(map[string][]store.Node)
	online := make([]store.Node, 0, len(nodes))

	for _, n := range nodes {
		if !store.Online(n, now) || n.PoolID == "" {
			continue
		}

		onlineByPool[n.PoolID] = append(
			onlineByPool[n.PoolID],
			n,
		)
		online = append(online, n)
	}

	// Pays owners before the early return below, so a pool whose last node
	// just went offline still gets its remaining minutes.
	if s.credits != nil {
		if err := s.credits.Tick(ctx, now, online); err != nil {
			return err
		}
	}

	// No eligible nodes are online.
	if len(onlineByPool) == 0 {
		return nil
	}

	// Retrieve jobs in oldest-first order.
	jobs, err := s.jobs.List(ctx)
	if err != nil {
		return err
	}

	// Maintain separate round-robin positions for each pool.
	nextByPool := make(map[string]int)

	for _, j := range jobs {

		// Only queued jobs can be scheduled.
		if j.Status != store.JobQueued {
			continue
		}

		// Legacy jobs without a pool cannot be scheduled.
		if j.PoolID == "" {
			continue
		}

		// Only nodes from this job's pool are eligible.
		candidates := onlineByPool[j.PoolID]

		// Leave the job queued when its pool has no online nodes.
		if len(candidates) == 0 {
			continue
		}

		// Only keep nodes that satisfy the job's OS requirement.
		filtered := make([]store.Node, 0, len(candidates))
		for _, n := range candidates {
			if validNode(n, j) {
				filtered = append(filtered, n)
			}
		}

		// Leave the job queued if no eligible nodes remain.
		if len(filtered) == 0 {
			continue
		}

		// Select the next online node within this pool.
		i := nextByPool[j.PoolID]
		node := filtered[i%len(filtered)]

		// Assign the job and count this placement attempt.
		j.Status = store.JobAssigned
		j.NodeID = node.ID
		j.Attempts++

		// Save the updated job.
		if err := s.jobs.Update(ctx, j); err != nil {
			return err
		}

		// Advance the round-robin position for this pool only.
		nextByPool[j.PoolID]++

		log.Printf(
			"assigned job %s to node %s in pool %s (attempt %d)",
			j.ID,
			j.NodeID,
			j.PoolID,
			j.Attempts,
		)
	}

	return nil
}

// requeueLost puts jobs back in the queue when the node they were
// placed on goes offline. Jobs that reached maxAttempts are failed.
//
// A missing node also counts as lost, since it cannot run the job.
func (s *Scheduler) requeueLost(ctx context.Context, now time.Time, nodes []store.Node) error {
	// On restart, stored heartbeats may be stale even though agents are still
	// running. Give them one NodeTimeout to heartbeat before recovering jobs.
	if now.Sub(s.started) < store.NodeTimeout {
		return nil
	}

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

		// Only active jobs can be lost.
		if j.Status != store.JobAssigned && j.Status != store.JobRunning {
			continue
		}

		// Leave jobs alone if their assigned node is online.
		if online[j.NodeID] {
			continue
		}

		lost := j.NodeID

		// Fail jobs that have already reached the attempt limit.
		if j.Attempts >= maxAttempts {
			j.Status = store.JobFailed
			finished := now.UTC()
			j.FinishedAt = &finished
		} else {
			j.Status = store.JobQueued
		}

		// The next node starts the clock fresh.
		j.NodeID = ""
		j.StartedAt = nil

		if err := s.jobs.Update(ctx, j); err != nil {
			return err
		}

		log.Printf(
			"node %s offline: job %s -> %s (attempt %d)",
			lost,
			j.ID,
			j.Status,
			j.Attempts,
		)
	}

	return nil
}

// Run continuously attempts to assign queued jobs to online nodes
// at the specified interval.
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
