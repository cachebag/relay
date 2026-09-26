package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Test:
// One online node, one queued job.
// After Tick(), the job should be assigned to that node.
func TestJobAssigned(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	node := store.Node{
		ID:       "node-1",
		LastSeen: time.Now(),
	}

	job := store.Job{
		ID:     "job-1",
		Status: store.JobQueued,
	}

	if err := nodes.Upsert(ctx, node); err != nil {
		t.Fatalf("adding node: %v", err)
	}

	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("adding job: %v", err)
	}

	s := New(nodes, jobs)

	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}

	if got[0].Status != store.JobAssigned {
		t.Errorf(
			"expected status %q, got %q",
			store.JobAssigned,
			got[0].Status,
		)
	}

	if got[0].NodeID != node.ID {
		t.Errorf(
			"expected NodeID %q, got %q",
			node.ID,
			got[0].NodeID,
		)
	}
}

// Test:
// One node whose LastSeen was 60 seconds ago.
// Since nodeTimeout is 30 seconds, the node is offline.
// The queued job should remain queued.
func TestNodeOffline(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	node := store.Node{
		ID:       "node-1",
		LastSeen: time.Now().Add(-60 * time.Second),
	}

	job := store.Job{
		ID:     "job-1",
		Status: store.JobQueued,
	}

	if err := nodes.Upsert(ctx, node); err != nil {
		t.Fatalf("adding node: %v", err)
	}

	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("adding job: %v", err)
	}

	s := New(nodes, jobs)

	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}

	if got[0].Status != store.JobQueued {
		t.Errorf(
			"expected job to remain %q, got %q",
			store.JobQueued,
			got[0].Status,
		)
	}

	if got[0].NodeID != "" {
		t.Errorf(
			"expected NodeID to remain empty, got %q",
			got[0].NodeID,
		)
	}
}

// Test:
// Two online nodes and three queued jobs,
// one node gets two jobs and the other gets one.
func TestJobDistribution(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	node1 := store.Node{
		ID:       "node-1",
		LastSeen: time.Now(),
	}

	node2 := store.Node{
		ID:       "node-2",
		LastSeen: time.Now(),
	}

	if err := nodes.Upsert(ctx, node1); err != nil {
		t.Fatalf("adding node 1: %v", err)
	}

	if err := nodes.Upsert(ctx, node2); err != nil {
		t.Fatalf("adding node 2: %v", err)
	}

	job1 := store.Job{
		ID:     "job-1",
		Status: store.JobQueued,
	}

	job2 := store.Job{
		ID:     "job-2",
		Status: store.JobQueued,
	}

	job3 := store.Job{
		ID:     "job-3",
		Status: store.JobQueued,
	}

	for _, job := range []store.Job{job1, job2, job3} {
		if err := jobs.Create(ctx, job); err != nil {
			t.Fatalf("adding job %s: %v", job.ID, err)
		}
	}

	s := New(nodes, jobs)

	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(got))
	}

	counts := make(map[string]int)

	for _, job := range got {
		if job.Status != store.JobAssigned {
			t.Errorf(
				"job %s: expected status %q, got %q",
				job.ID,
				store.JobAssigned,
				job.Status,
			)
		}

		counts[job.NodeID]++
	}

	// We expect one node to have two jobs and
	// the other node to have one.
	node1Count := counts[node1.ID]
	node2Count := counts[node2.ID]

	if !((node1Count == 2 && node2Count == 1) ||
		(node1Count == 1 && node2Count == 2)) {

		t.Errorf(
			"expected jobs split 2/1 between nodes; node-1=%d node-2=%d",
			node1Count,
			node2Count,
		)
	}
}

// Test:
// There are online nodes, but there are no queued jobs.
// Tick() should return nil and leave existing jobs unchanged.
func TestNoJobsQueued(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	node := store.Node{
		ID:       "node-1",
		LastSeen: time.Now(),
	}

	job := store.Job{
		ID:     "job-1",
		Status: store.JobAssigned,
		NodeID: "node-1",
	}

	if err := nodes.Upsert(ctx, node); err != nil {
		t.Fatalf("adding node: %v", err)
	}

	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("adding job: %v", err)
	}

	s := New(nodes, jobs)

	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("expected Tick() to return nil, got %v", err)
	}

	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}

	if got[0].Status != store.JobAssigned {
		t.Errorf(
			"expected status to remain %q, got %q",
			store.JobAssigned,
			got[0].Status,
		)
	}

	if got[0].NodeID != "node-1" {
		t.Errorf(
			"expected NodeID to remain %q, got %q",
			"node-1",
			got[0].NodeID,
		)
	}
}
