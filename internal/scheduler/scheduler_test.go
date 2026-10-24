package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// tickAndGet creates temporary stores, runs one scheduling
// tick, and retrieves the requested job.
func tickAndGet(
	t *testing.T,
	now time.Time,
	nodes []store.Node,
	jobs []store.Job,
	id string,
) store.Job {
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

	if err := New(reg, js, nil).Tick(ctx, now); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	got, err := js.Get(ctx, id)
	if err != nil {
		t.Fatalf("getting job %s: %v", id, err)
	}

	return got
}

// One online node and one queued job.
// The job should be assigned to its pool's node.
func TestJobAssigned(t *testing.T) {
	now := time.Now()

	node := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now,
	}

	job := store.Job{
		ID:     "job-1",
		PoolID: "pool-a",
		Status: store.JobQueued,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{node},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobAssigned {
		t.Errorf("expected assigned, got %q", got.Status)
	}
	if got.NodeID != node.ID {
		t.Errorf("expected node %q, got %q", node.ID, got.NodeID)
	}
	if got.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", got.Attempts)
	}
}

// An offline node cannot receive a queued job.
func TestNodeOffline(t *testing.T) {
	now := time.Now()

	node := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now.Add(-60 * time.Second),
	}

	job := store.Job{
		ID:     "job-1",
		PoolID: "pool-a",
		Status: store.JobQueued,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{node},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobQueued {
		t.Errorf("expected queued, got %q", got.Status)
	}
	if got.NodeID != "" {
		t.Errorf("expected no assigned node, got %q", got.NodeID)
	}
	if got.Attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", got.Attempts)
	}
}

// Three queued jobs should be split 2/1 between two
// online nodes belonging to the same pool.
func TestJobDistribution(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	testNodes := []store.Node{
		{ID: "node-1", Hostname: "node-1", PoolID: "pool-a", LastSeen: now},
		{ID: "node-2", Hostname: "node-2", PoolID: "pool-a", LastSeen: now},
	}

	for _, n := range testNodes {
		if err := nodes.Upsert(ctx, n); err != nil {
			t.Fatalf("adding node: %v", err)
		}
	}

	for _, id := range []string{"job-1", "job-2", "job-3"} {
		j := store.Job{
			ID:     id,
			PoolID: "pool-a",
			Status: store.JobQueued,
		}
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatalf("adding job: %v", err)
		}
	}

	if err := New(nodes, jobs, nil).Tick(ctx, now); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	all, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(all))
	}

	counts := make(map[string]int)

	for _, j := range all {
		if j.Status != store.JobAssigned {
			t.Errorf("job %s was not assigned", j.ID)
		}
		if j.PoolID != "pool-a" {
			t.Errorf("job %s has incorrect pool", j.ID)
		}
		if j.Attempts != 1 {
			t.Errorf("job %s: expected 1 attempt", j.ID)
		}
		counts[j.NodeID]++
	}

	if counts["node-1"] != 2 || counts["node-2"] != 1 {
		t.Errorf("expected 2/1 distribution, got %v", counts)
	}
}

// Jobs already assigned to an online node must not
// be scheduled again.
func TestNoJobsQueued(t *testing.T) {
	now := time.Now()

	node := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now,
	}

	job := store.Job{
		ID:       "job-1",
		PoolID:   "pool-a",
		Status:   store.JobAssigned,
		NodeID:   "node-1",
		Attempts: 1,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{node},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobAssigned {
		t.Errorf("expected assigned, got %q", got.Status)
	}
	if got.NodeID != node.ID {
		t.Errorf("expected node %q, got %q", node.ID, got.NodeID)
	}
	if got.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", got.Attempts)
	}
}

// A job assigned to an offline node returns to the queue.
func TestRequeueLost(t *testing.T) {
	now := time.Now()

	offline := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now.Add(-60 * time.Second),
	}

	job := store.Job{
		ID:       "job-1",
		PoolID:   "pool-a",
		Status:   store.JobAssigned,
		NodeID:   "node-1",
		Attempts: 1,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{offline},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobQueued {
		t.Errorf("expected queued, got %q", got.Status)
	}
	if got.NodeID != "" {
		t.Errorf("expected cleared NodeID, got %q", got.NodeID)
	}
	if got.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", got.Attempts)
	}
}

// A lost job may be reassigned to another online node
// in the same pool during the same tick.
func TestRequeueLostReassigned(t *testing.T) {
	now := time.Now()

	offline := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now.Add(-60 * time.Second),
	}

	online := store.Node{
		ID:       "node-2",
		PoolID:   "pool-a",
		LastSeen: now,
	}

	job := store.Job{
		ID:       "job-1",
		PoolID:   "pool-a",
		Status:   store.JobRunning,
		NodeID:   "node-1",
		Attempts: 1,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{offline, online},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobAssigned {
		t.Errorf("expected assigned, got %q", got.Status)
	}
	if got.NodeID != online.ID {
		t.Errorf("expected node %q, got %q", online.ID, got.NodeID)
	}
	if got.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", got.Attempts)
	}
}

// Jobs that have reached maxAttempts fail instead
// of being requeued again.
func TestRequeueLostFailsAfterMaxAttempts(t *testing.T) {
	now := time.Now()

	offline := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now.Add(-60 * time.Second),
	}

	online := store.Node{
		ID:       "node-2",
		PoolID:   "pool-a",
		LastSeen: now,
	}

	job := store.Job{
		ID:       "job-1",
		PoolID:   "pool-a",
		Status:   store.JobAssigned,
		NodeID:   "node-1",
		Attempts: maxAttempts,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{offline, online},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobFailed {
		t.Errorf("expected failed, got %q", got.Status)
	}
	if got.NodeID != "" {
		t.Errorf("expected cleared NodeID, got %q", got.NodeID)
	}
	if got.Attempts != maxAttempts {
		t.Errorf("expected %d attempts, got %d", maxAttempts, got.Attempts)
	}
}

// A job assigned to an unknown node is treated
// as lost and returned to the queue.
func TestRequeueLostUnknownNode(t *testing.T) {
	now := time.Now()

	job := store.Job{
		ID:       "job-1",
		PoolID:   "pool-a",
		Status:   store.JobAssigned,
		NodeID:   "ghost",
		Attempts: 1,
	}

	got := tickAndGet(t, now, nil, []store.Job{job}, job.ID)

	if got.Status != store.JobQueued {
		t.Errorf("expected queued, got %q", got.Status)
	}
	if got.NodeID != "" {
		t.Errorf("expected cleared NodeID, got %q", got.NodeID)
	}
}

// Active jobs on online nodes and finished jobs
// should remain unchanged during recovery.
func TestRequeueLostUntouched(t *testing.T) {
	now := time.Now()

	online := store.Node{
		ID:       "node-1",
		PoolID:   "pool-a",
		LastSeen: now,
	}
	offline := store.Node{
		ID:       "node-2",
		PoolID:   "pool-a",
		LastSeen: now.Add(-60 * time.Second),
	}

	cases := []store.Job{
		{ID: "running-online", PoolID: "pool-a", Status: store.JobRunning, NodeID: "node-1", Attempts: 1},
		{ID: "assigned-online", PoolID: "pool-a", Status: store.JobAssigned, NodeID: "node-1", Attempts: 1},
		{ID: "done-offline", PoolID: "pool-a", Status: store.JobDone, NodeID: "node-2", Attempts: 1},
		{ID: "failed-offline", PoolID: "pool-a", Status: store.JobFailed, NodeID: "node-2", Attempts: 3},
	}

	for _, want := range cases {
		t.Run(want.ID, func(t *testing.T) {
			got := tickAndGet(
				t, now,
				[]store.Node{online, offline},
				[]store.Job{want},
				want.ID,
			)

			if got.Status != want.Status {
				t.Errorf("expected status %q, got %q", want.Status, got.Status)
			}
			if got.NodeID != want.NodeID {
				t.Errorf("expected node %q, got %q", want.NodeID, got.NodeID)
			}
			if got.Attempts != want.Attempts {
				t.Errorf("expected %d attempts, got %d", want.Attempts, got.Attempts)
			}
		})
	}
}

// A job belonging to Pool A must only be assigned
// to a Pool A node, and likewise for Pool B.
func TestJobsStayInTheirPool(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	testNodes := []store.Node{
		{ID: "node-a", Hostname: "node-a", PoolID: "pool-a", LastSeen: now},
		{ID: "node-b", Hostname: "node-b", PoolID: "pool-b", LastSeen: now},
	}

	testJobs := []store.Job{
		{ID: "job-a", PoolID: "pool-a", Status: store.JobQueued},
		{ID: "job-b", PoolID: "pool-b", Status: store.JobQueued},
	}

	for _, n := range testNodes {
		if err := nodes.Upsert(ctx, n); err != nil {
			t.Fatalf("adding node: %v", err)
		}
	}

	for _, j := range testJobs {
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatalf("adding job: %v", err)
		}
	}

	if err := New(nodes, jobs, nil).Tick(ctx, now); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	expected := map[string]string{
		"job-a": "node-a",
		"job-b": "node-b",
	}

	for jobID, nodeID := range expected {
		got, err := jobs.Get(ctx, jobID)
		if err != nil {
			t.Fatalf("getting job %s: %v", jobID, err)
		}

		if got.Status != store.JobAssigned {
			t.Errorf("job %s not assigned", jobID)
		}
		if got.NodeID != nodeID {
			t.Errorf("job %s: expected %q, got %q",
				jobID, nodeID, got.NodeID)
		}
		if got.Attempts != 1 {
			t.Errorf("job %s: expected 1 attempt", jobID)
		}
	}
}

// A job stays queued when its pool has no online node,
// even if another pool has an available node.
func TestJobWaitsForOwnPool(t *testing.T) {
	now := time.Now()

	node := store.Node{
		ID:       "node-b",
		PoolID:   "pool-b",
		LastSeen: now,
	}

	job := store.Job{
		ID:     "job-a",
		PoolID: "pool-a",
		Status: store.JobQueued,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{node},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobQueued {
		t.Errorf("expected queued, got %q", got.Status)
	}
	if got.NodeID != "" {
		t.Errorf("expected no assignment, got %q", got.NodeID)
	}
	if got.Attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", got.Attempts)
	}
}

// Round-robin scheduling must operate independently
// within each pool.
func TestRoundRobinPerPool(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	testNodes := []store.Node{
		{ID: "a1", Hostname: "a1", PoolID: "pool-a", LastSeen: now},
		{ID: "a2", Hostname: "a2", PoolID: "pool-a", LastSeen: now},
		{ID: "b1", Hostname: "b1", PoolID: "pool-b", LastSeen: now},
		{ID: "b2", Hostname: "b2", PoolID: "pool-b", LastSeen: now},
	}

	for _, n := range testNodes {
		if err := nodes.Upsert(ctx, n); err != nil {
			t.Fatalf("adding node: %v", err)
		}
	}

	testJobs := []store.Job{
		{ID: "a-job-1", PoolID: "pool-a", Status: store.JobQueued},
		{ID: "a-job-2", PoolID: "pool-a", Status: store.JobQueued},
		{ID: "a-job-3", PoolID: "pool-a", Status: store.JobQueued},
		{ID: "b-job-1", PoolID: "pool-b", Status: store.JobQueued},
		{ID: "b-job-2", PoolID: "pool-b", Status: store.JobQueued},
	}

	for _, j := range testJobs {
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatalf("adding job: %v", err)
		}
	}

	if err := New(nodes, jobs, nil).Tick(ctx, now); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	expected := map[string]string{
		"a-job-1": "a1",
		"a-job-2": "a2",
		"a-job-3": "a1",
		"b-job-1": "b1",
		"b-job-2": "b2",
	}

	for jobID, expectedNode := range expected {
		got, err := jobs.Get(ctx, jobID)
		if err != nil {
			t.Fatalf("getting %s: %v", jobID, err)
		}

		if got.Status != store.JobAssigned {
			t.Errorf("%s: expected assigned, got %q",
				jobID, got.Status)
		}
		if got.NodeID != expectedNode {
			t.Errorf("%s: expected node %q, got %q",
				jobID, expectedNode, got.NodeID)
		}
		if got.Attempts != 1 {
			t.Errorf("%s: expected 1 attempt, got %d",
				jobID, got.Attempts)
		}
	}
}

// Jobs created before pool ownership existed
// must not be scheduled onto modern pool nodes.
func TestLegacyJobWithoutPoolStaysQueued(t *testing.T) {
	now := time.Now()

	node := store.Node{
		ID:       "node-a",
		PoolID:   "pool-a",
		LastSeen: now,
	}

	job := store.Job{
		ID:     "legacy-job",
		Status: store.JobQueued,
	}

	got := tickAndGet(
		t, now,
		[]store.Node{node},
		[]store.Job{job},
		job.ID,
	)

	if got.Status != store.JobQueued {
		t.Errorf("expected queued, got %q", got.Status)
	}
	if got.NodeID != "" {
		t.Errorf("expected no node, got %q", got.NodeID)
	}
	if got.Attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", got.Attempts)
	}
}

// Requeuing a lost job clears started_at so the next node starts the clock
// fresh, and giving up on it stamps finished_at.
func TestRequeueLostResetsClock(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	offline := store.Node{ID: "node-1", PoolID: "pool-a", LastSeen: now.Add(-60 * time.Second)}

	requeued := tickAndGet(t, now, []store.Node{offline}, []store.Job{{
		ID: "job-1", PoolID: "pool-a", Status: store.JobRunning, NodeID: "node-1", Attempts: 1, StartedAt: &started,
	}}, "job-1")
	if requeued.Status != store.JobQueued || requeued.StartedAt != nil || requeued.FinishedAt != nil {
		t.Errorf("requeued job: status=%s started=%v finished=%v, want queued/nil/nil", requeued.Status, requeued.StartedAt, requeued.FinishedAt)
	}

	failed := tickAndGet(t, now, []store.Node{offline}, []store.Job{{
		ID: "job-2", PoolID: "pool-a", Status: store.JobRunning, NodeID: "node-1", Attempts: maxAttempts, StartedAt: &started,
	}}, "job-2")
	if failed.Status != store.JobFailed || failed.FinishedAt == nil || failed.StartedAt != nil {
		t.Errorf("failed job: status=%s started=%v finished=%v, want failed/nil/set", failed.Status, failed.StartedAt, failed.FinishedAt)
	}
}

// osNodes returns one online Linux node and one online Windows node in pool-a.
// The registry sorts by ID, so Linux is first in the rotation.
func osNodes(now time.Time) []store.Node {
	return []store.Node{
		{ID: "node-a-linux", PoolID: "pool-a", OS: "linux", LastSeen: now},
		{ID: "node-b-windows", PoolID: "pool-a", OS: "windows", LastSeen: now},
	}
}

// job-1 takes the Linux node, leaving round-robin pointed at Windows.
// job-2 requires Linux, so it must still be assigned to the Linux node.
func TestRequiresOSOverridesRoundRobin(t *testing.T) {
	now := time.Now()
	jobs := []store.Job{
		{ID: "job-1", PoolID: "pool-a", Status: store.JobQueued},
		{
			ID:       "job-2",
			PoolID:   "pool-a",
			Status:   store.JobQueued,
			Requires: store.Requires{OS: "linux"},
		},
	}

	got := tickAndGet(t, now, osNodes(now), jobs, "job-2")
	if got.Status != store.JobAssigned || got.NodeID != "node-a-linux" {
		t.Errorf("expected job assigned to node-a-linux, got status %q on node %q", got.Status, got.NodeID)
	}
}

// A job requiring an OS that no online node runs stays queued.
func TestRequiresOSNoFittingNodeStaysQueued(t *testing.T) {
	now := time.Now()
	job := store.Job{
		ID:       "job-1",
		PoolID:   "pool-a",
		Status:   store.JobQueued,
		Requires: store.Requires{OS: "darwin"},
	}

	got := tickAndGet(t, now, osNodes(now), []store.Job{job}, job.ID)
	if got.Status != store.JobQueued || got.NodeID != "" || got.Attempts != 0 {
		t.Errorf("expected untouched queued job, got %+v", got)
	}
}

// A job that cannot be placed must not advance round-robin. The next
// unrestricted job should still be assigned to the first node.
func TestStuckJobDoesNotAdvanceRoundRobin(t *testing.T) {
	now := time.Now()
	jobs := []store.Job{
		{
			ID:       "job-1",
			PoolID:   "pool-a",
			Status:   store.JobQueued,
			Requires: store.Requires{OS: "darwin"},
		},
		{ID: "job-2", PoolID: "pool-a", Status: store.JobQueued},
	}

	got := tickAndGet(t, now, osNodes(now), jobs, "job-2")
	if got.NodeID != "node-a-linux" {
		t.Errorf("expected node-a-linux, got %q", got.NodeID)
	}
}

// Jobs without requirements still round-robin across both nodes.
func TestNoRequirementStillRoundRobins(t *testing.T) {
	now := time.Now()
	jobs := []store.Job{
		{ID: "job-1", PoolID: "pool-a", Status: store.JobQueued},
		{ID: "job-2", PoolID: "pool-a", Status: store.JobQueued},
	}

	first := tickAndGet(t, now, osNodes(now), jobs, "job-1")
	second := tickAndGet(t, now, osNodes(now), jobs, "job-2")

	if first.NodeID != "node-a-linux" || second.NodeID != "node-b-windows" {
		t.Errorf("expected Linux then Windows, got %q then %q", first.NodeID, second.NodeID)
	}
}
