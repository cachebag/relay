package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// priorityLedger keeps every entry so tests can check balances and payments.
type priorityLedger struct {
	entries []store.CreditEntry
}

func (l *priorityLedger) Add(_ context.Context, e store.CreditEntry) error {
	l.entries = append(l.entries, e)
	return nil
}

func (l *priorityLedger) Balances(_ context.Context, poolID string) (map[string]int, error) {
	out := make(map[string]int)
	for _, e := range l.entries {
		if e.PoolID == poolID {
			out[e.Account] += e.Delta
		}
	}
	return out, nil
}

// priorityTick runs one tick and returns the stored jobs so tests check
// actual placements and the jobs left waiting, rather than log order.
func priorityTick(
	t *testing.T,
	now time.Time,
	nodes []store.Node,
	jobs []store.Job,
	l *priorityLedger,
) store.JobStore {
	t.Helper()

	ctx := context.Background()
	reg := store.NewMem()
	js := store.NewMemJobs()

	for _, n := range nodes {
		if err := reg.Upsert(ctx, n); err != nil {
			t.Fatalf("adding node %s: %v", n.ID, err)
		}
	}

	for _, j := range jobs {
		if err := js.Create(ctx, j); err != nil {
			t.Fatalf("adding job %s: %v", j.ID, err)
		}
	}

	var s *Scheduler
	if l == nil {
		// Use a literal nil so New(reg, jobs, earner, nil) keeps the baseline
		// oldest-first behavior.
		s = New(reg, js, nil, nil)
	} else {
		s = New(reg, js, nil, l)
	}
	s.started = now.Add(-store.NodeTimeout)

	if err := s.Tick(ctx, now); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	return js
}

// checkPlacement checks that waiting does not consume an attempt or bind a
// job to a node, and that an assigned job has exactly one placement attempt.
func checkPlacement(t *testing.T, jobs store.JobStore, id, nodeID string) {
	t.Helper()
	j, err := jobs.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	status, attempts := store.JobQueued, 0
	if nodeID != "" {
		status, attempts = store.JobAssigned, 1
	}
	if j.Status != status || j.NodeID != nodeID || j.Attempts != attempts {
		t.Errorf("job %s = %s on %q (attempt %d), want %s on %q (attempt %d)", id, j.Status, j.NodeID, j.Attempts, status, nodeID, attempts)
	}
}

// TestBalanceOrder checks that a newer job from a higher balance is placed
// before an older job from a lower balance when one node is available.
func TestBalanceOrder(t *testing.T) {
	now := time.Now().UTC()

	older := store.Job{
		ID:        "older",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "omar",
		CreatedAt: now.Add(-2 * time.Minute),
	}
	newer := store.Job{
		ID:        "newer",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-1 * time.Minute),
	}

	l := &priorityLedger{}
	entryTime := now.Add(-3 * time.Minute)
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-akrm",
		PoolID:    "pool-a",
		Account:   "akrm",
		Delta:     10,
		Reason:    "online",
		CreatedAt: entryTime,
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-omar",
		PoolID:    "pool-a",
		Account:   "omar",
		Delta:     -5,
		Reason:    "job",
		CreatedAt: entryTime,
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}

	nodes := []store.Node{
		{ID: "node-a", PoolID: "pool-a", LastSeen: now},
	}
	jobs := priorityTick(t, now, nodes, []store.Job{older, newer}, l)
	checkPlacement(t, jobs, "newer", "node-a")
	checkPlacement(t, jobs, "older", "")
}

// TestSameBalanceOlderFirst checks that equal balances keep created-at order.
func TestSameBalanceOlderFirst(t *testing.T) {
	now := time.Now().UTC()

	older := store.Job{
		ID:        "older",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-2 * time.Minute),
	}
	newer := store.Job{
		ID:        "newer",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-1 * time.Minute),
	}

	l := &priorityLedger{}
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-akrm",
		PoolID:    "pool-a",
		Account:   "akrm",
		Delta:     5,
		Reason:    "online",
		CreatedAt: now.Add(-3 * time.Minute),
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}

	nodes := []store.Node{
		{ID: "node-a", PoolID: "pool-a", LastSeen: now},
	}
	jobs := priorityTick(t, now, nodes, []store.Job{older, newer}, l)
	checkPlacement(t, jobs, "older", "node-a")
	checkPlacement(t, jobs, "newer", "")
}

// TestNoLedgerEntryIsZero checks that an absent submitter sorts as 0,
// between a positive and a negative balance.
func TestNoLedgerEntryIsZero(t *testing.T) {
	now := time.Now().UTC()

	l := &priorityLedger{}
	entryTime := now.Add(-3 * time.Minute)
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-akrm",
		PoolID:    "pool-a",
		Account:   "akrm",
		Delta:     10,
		Reason:    "online",
		CreatedAt: entryTime,
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-omar",
		PoolID:    "pool-a",
		Account:   "omar",
		Delta:     -5,
		Reason:    "job",
		CreatedAt: entryTime,
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}

	high := store.Job{
		ID:        "high",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-1 * time.Minute),
	}
	zero := store.Job{
		ID:        "zero",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "nobody",
		CreatedAt: now.Add(-2 * time.Minute),
	}
	low := store.Job{
		ID:        "low",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "omar",
		CreatedAt: now.Add(-3 * time.Minute),
	}

	nodes := []store.Node{
		{ID: "node-a", PoolID: "pool-a", LastSeen: now},
		{ID: "node-b", PoolID: "pool-a", LastSeen: now},
	}
	jobs := priorityTick(t, now, nodes, []store.Job{low, zero, high}, l)
	checkPlacement(t, jobs, "high", "node-a")
	checkPlacement(t, jobs, "zero", "node-b")
	checkPlacement(t, jobs, "low", "")
}

// An idle node with the wrong OS cannot take a job from a busy eligible node.
// That waiting job must not stop other jobs from using the idle node.
func TestPriorityWaitsForEligibleCapacity(t *testing.T) {
	for _, status := range []store.JobStatus{store.JobAssigned, store.JobRunning} {
		t.Run(string(status), func(t *testing.T) {
			now := time.Now().UTC()
			jobs := priorityTick(t, now, osNodes(now), []store.Job{
				{ID: "active", PoolID: "pool-a", Status: status, NodeID: "node-a-linux", Attempts: 1, CreatedAt: now.Add(-time.Hour)},
				{ID: "waiting-linux", PoolID: "pool-a", Status: store.JobQueued, Requires: store.Requires{OS: "linux"}, CreatedAt: now.Add(-time.Minute)},
				{ID: "ready-windows", PoolID: "pool-a", Status: store.JobQueued, Requires: store.Requires{OS: "windows"}, CreatedAt: now},
			}, &priorityLedger{})
			checkPlacement(t, jobs, "waiting-linux", "")
			checkPlacement(t, jobs, "ready-windows", "node-b-windows")
		})
	}
}

// TestTwoPoolsIndependent checks that each pool is ordered only by its own
// ledger, even though the scheduler places jobs from both pools in one tick.
func TestTwoPoolsIndependent(t *testing.T) {
	now := time.Now().UTC()

	l := &priorityLedger{}
	entryTime := now.Add(-3 * time.Minute)

	entries := []store.CreditEntry{
		{ID: "a-akrm", PoolID: "pool-a", Account: "akrm", Delta: 10, Reason: "online", CreatedAt: entryTime},
		{ID: "a-omar", PoolID: "pool-a", Account: "omar", Delta: -5, Reason: "job", CreatedAt: entryTime},
		{ID: "b-omar", PoolID: "pool-b", Account: "omar", Delta: 10, Reason: "online", CreatedAt: entryTime},
		{ID: "b-akrm", PoolID: "pool-b", Account: "akrm", Delta: -5, Reason: "job", CreatedAt: entryTime},
	}
	for _, e := range entries {
		if err := l.Add(context.Background(), e); err != nil {
			t.Fatalf("adding credit: %v", err)
		}
	}

	aHigh := store.Job{
		ID:        "a-high",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-1 * time.Minute),
	}
	aLow := store.Job{
		ID:        "a-low",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "omar",
		CreatedAt: now.Add(-2 * time.Minute),
	}
	bHigh := store.Job{
		ID:        "b-high",
		PoolID:    "pool-b",
		Status:    store.JobQueued,
		Submitter: "omar",
		CreatedAt: now.Add(-1 * time.Minute),
	}
	bLow := store.Job{
		ID:        "b-low",
		PoolID:    "pool-b",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-2 * time.Minute),
	}

	nodes := []store.Node{
		{ID: "node-a", PoolID: "pool-a", LastSeen: now},
		{ID: "node-b", PoolID: "pool-b", LastSeen: now},
	}
	jobs := priorityTick(t, now, nodes, []store.Job{aHigh, aLow, bHigh, bLow}, l)
	checkPlacement(t, jobs, "a-high", "node-a")
	checkPlacement(t, jobs, "a-low", "")
	checkPlacement(t, jobs, "b-high", "node-b")
	checkPlacement(t, jobs, "b-low", "")
}

// TestEnoughNodesPlacesEveryone checks that a balance order still assigns
// every queued job when each job has an online node.
func TestEnoughNodesPlacesEveryone(t *testing.T) {
	now := time.Now().UTC()

	l := &priorityLedger{}
	entryTime := now.Add(-3 * time.Minute)
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-akrm",
		PoolID:    "pool-a",
		Account:   "akrm",
		Delta:     10,
		Reason:    "online",
		CreatedAt: entryTime,
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}
	if err := l.Add(context.Background(), store.CreditEntry{
		ID:        "credit-omar",
		PoolID:    "pool-a",
		Account:   "omar",
		Delta:     -5,
		Reason:    "job",
		CreatedAt: entryTime,
	}); err != nil {
		t.Fatalf("adding credit: %v", err)
	}

	high := store.Job{
		ID:        "high",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-1 * time.Minute),
	}
	low := store.Job{
		ID:        "low",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "omar",
		CreatedAt: now.Add(-2 * time.Minute),
	}

	nodes := []store.Node{
		{ID: "node-a", PoolID: "pool-a", LastSeen: now},
		{ID: "node-b", PoolID: "pool-a", LastSeen: now},
	}
	jobs := priorityTick(t, now, nodes, []store.Job{low, high}, l)
	checkPlacement(t, jobs, "high", "node-a")
	checkPlacement(t, jobs, "low", "node-b")
}

// TestNilLedgerKeepsOldestFirst checks that New(reg, jobs, earner, nil) still
// uses today's oldest-first behavior.
func TestNilLedgerKeepsOldestFirst(t *testing.T) {
	now := time.Now().UTC()

	older := store.Job{
		ID:        "older",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "akrm",
		CreatedAt: now.Add(-2 * time.Minute),
	}
	newer := store.Job{
		ID:        "newer",
		PoolID:    "pool-a",
		Status:    store.JobQueued,
		Submitter: "omar",
		CreatedAt: now.Add(-1 * time.Minute),
	}

	nodes := []store.Node{
		{ID: "node-a", PoolID: "pool-a", LastSeen: now},
		{ID: "node-b", PoolID: "pool-a", LastSeen: now},
	}
	jobs := priorityTick(t, now, nodes, []store.Job{older, newer}, nil)
	checkPlacement(t, jobs, "older", "node-a")
	checkPlacement(t, jobs, "newer", "node-b")
}
