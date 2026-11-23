package scheduler

import (
	"context"
	"fmt"
	"log"
	"sort"
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
	ledger  store.CreditStore
	started time.Time

	// next is the round-robin position per pool, used to break ties between
	// equally loaded nodes. It lives here so it carries across ticks.
	next map[string]int
}

// New returns a Scheduler. credits pays owners for their machines' online
// time each tick; nil turns that off. ledger is the credit store used to
// order queued jobs by submitter balance; nil keeps oldest-first ordering.
func New(nodes store.Registry, jobs store.JobStore, credits *credit.Earner, ledgers ...store.CreditStore) *Scheduler {
	var ledger store.CreditStore = nil
	if len(ledgers) > 0 {
		ledger = ledgers[0]
	}

	return &Scheduler{
		nodes:   nodes,
		jobs:    jobs,
		credits: credits,
		ledger:  ledger,
		started: time.Now(),
		next:    make(map[string]int),
	}
}

// nodeFits reports whether a node satisfies a job's requirements. Only the
// OS for now; the approved-job-types ticket extends it.
func nodeFits(n store.Node, j store.Job) bool {
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
	jobs, err := s.jobs.ListActive(ctx)
	if err != nil {
		return err
	}

	// How many jobs each node already holds. A node runs one job at a time,
	// so a job placed on a busy node waits behind it while idle nodes sit.
	load := make(map[string]int)
	for _, j := range jobs {
		if j.NodeID != "" && (j.Status == store.JobAssigned || j.Status == store.JobRunning) {
			load[j.NodeID]++
		}
	}
	if s.next == nil {
		s.next = make(map[string]int)
	}

	// Load each pool's balances once, for pools that have queued jobs.
	// Jobs from a submitter with no ledger entries get a balance of 0.
	var poolBalances map[string]map[string]int = make(map[string]map[string]int)
	var queue []store.Job
	for _, j := range jobs {
		if j.Status != store.JobQueued || j.PoolID == "" {
			continue
		}

		queue = append(queue, j)

		if s.ledger == nil {
			continue
		}

		if _, ok := poolBalances[j.PoolID]; ok {
			continue
		}

		var balances map[string]int
		balances, err := s.ledger.Balances(ctx, j.PoolID)
		if err != nil {
			return err
		}
		poolBalances[j.PoolID] = balances
	}

	// Highest balance goes first. Equal balances stay in created-at order,
	// and an absent ledger entry counts as 0.
	if s.ledger != nil {
		sort.Slice(queue, func(i, j int) bool {
			a := queue[i]
			b := queue[j]

			// Keep pools together so one pool's balances never order another's jobs.
			if a.PoolID != b.PoolID {
				return a.PoolID < b.PoolID
			}

			var ab int = 0
			if pool, ok := poolBalances[a.PoolID]; ok {
				if v, found := pool[a.Submitter]; found {
					ab = v
				}
			}

			var bb int = 0
			if pool, ok := poolBalances[b.PoolID]; ok {
				if v, found := pool[b.Submitter]; found {
					bb = v
				}
			}

			if ab != bb {
				return ab > bb
			}
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.Before(b.CreatedAt)
			}
			return a.ID < b.ID
		})
	}

	// Log why jobs waited, once per pool, when more than one submitter is
	// queued. The ledger may be absent, so the balance shown is 0.
	var waiting map[string][]string = make(map[string][]string)
	for _, j := range queue {
		account := j.Submitter
		if account == "" {
			account = "-"
		}

		accounts := waiting[j.PoolID]
		var found bool = false
		for _, a := range accounts {
			if a == account {
				found = true
				break
			}
		}
		if !found {
			waiting[j.PoolID] = append(accounts, account)
		}
	}

	if s.ledger != nil {
		for pool, accounts := range waiting {
			if len(accounts) <= 1 {
				continue
			}

			var line string = ""
			for _, account := range accounts {
				var balance int = 0
				if balances, ok := poolBalances[pool]; ok {
					if v, found := balances[account]; found {
						balance = v
					}
				}
				if line != "" {
					line += ", "
				}
				line += fmt.Sprintf("%s (%d)", account, balance)
			}
			log.Printf("queue order in pool %s: %s", pool, line)
		}
	}

	for _, j := range queue {

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
			if nodeFits(n, j) {
				filtered = append(filtered, n)
			}
		}

		// Leave the job queued if no eligible nodes remain.
		if len(filtered) == 0 {
			continue
		}

		// Pick the least-loaded eligible node. Ties go round-robin so equal
		// nodes take turns across ticks instead of the first one always winning.
		least := make([]store.Node, 0, len(filtered))
		for _, n := range filtered {
			if len(least) == 0 || load[n.ID] < load[least[0].ID] {
				least = least[:0]
			}
			if len(least) == 0 || load[n.ID] == load[least[0].ID] {
				least = append(least, n)
			}
		}
		// With credit priority enabled, leave excess work in the pool queue
		// until a node is free. Agent pickup uses created-at order, so stacking
		// assignments on a busy node would discard the priority order.
		// A nil ledger preserves the legacy oldest-first placement behavior.
		if s.ledger != nil && load[least[0].ID] > 0 {
			continue
		}
		node := least[s.next[j.PoolID]%len(least)]
		s.next[j.PoolID]++

		// Assign the job and count this placement attempt.
		j.Status = store.JobAssigned
		j.NodeID = node.ID
		j.Attempts++

		// Save the updated job.
		if err := s.jobs.Update(ctx, j); err != nil {
			return err
		}

		log.Printf(
			"assigned job %s to node %s (%d running) in pool %s (attempt %d)",
			j.ID,
			j.NodeID,
			load[node.ID],
			j.PoolID,
			j.Attempts,
		)
		load[node.ID]++
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

	jobs, err := s.jobs.ListActive(ctx)
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
