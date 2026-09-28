package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Test:
// One online node, one queued job.
// The job should have its Status updated to JobAssigned and its NodeID
// set to the online node's ID.
func TestJobAssigned(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	// Creation of an online node.
	node := store.Node{
		ID:       "node-1",
		LastSeen: time.Now(),
	}

	// Creation of queued job.
	job := store.Job{
		ID:     "job-1",
		Status: store.JobQueued,
	}

	// Adds the node to the node store.
	if err := nodes.Upsert(ctx, node); err != nil {
		t.Fatalf("adding node: %v", err)
	}

	// Adds the job to the job store.
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("adding job: %v", err)
	}

	// Creates a new scheduler with the previously created node
	// and job stores.
	s := New(nodes, jobs)

	// Calls Tick() to attempt job assignment.
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	// Retrieves the list of jobs from the job store to verify assignment.
	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	// Testing whether exactly one job was retrieved.
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}

	// Testing that the job was assigned to the correct node.
	if got[0].Status != store.JobAssigned {
		t.Errorf(
			"expected status %q, got %q",
			store.JobAssigned,
			got[0].Status,
		)
	}

	// Testing that the job was assigned to the correct node.
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
// The queued job should remain queued and its NodeID should remain empty.
func TestNodeOffline(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	// Creation of an offline node.
	node := store.Node{
		ID:       "node-1",
		LastSeen: time.Now().Add(-60 * time.Second),
	}

	// Creation of a queued job.
	job := store.Job{
		ID:     "job-1",
		Status: store.JobQueued,
	}

	// Adds the offline node to the node store.
	if err := nodes.Upsert(ctx, node); err != nil {
		t.Fatalf("adding node: %v", err)
	}

	// Adds the queued job to the job store.
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("adding job: %v", err)
	}

	// Creates a new scheduler with the previously created node
	// and job stores.
	s := New(nodes, jobs)

	// Calls Tick() to attempt job assignment.
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	// Retrieves the list of jobs from the job store to verify that
	// the offline node did not get assigned any jobs.
	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	// Testing whether exactly one job was retrieved.
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}

	// Testing that the job remains queued since the node is offline.
	if got[0].Status != store.JobQueued {
		t.Errorf(
			"expected job to remain %q, got %q",
			store.JobQueued,
			got[0].Status,
		)
	}

	// Testing that the NodeID remains empty since the node is offline.
	if got[0].NodeID != "" {
		t.Errorf(
			"expected NodeID to remain empty, got %q",
			got[0].NodeID,
		)
	}
}

// Test:
// Two online nodes and three queued jobs.
// One node gets two jobs and the other gets one.
func TestJobDistribution(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	// Creation of two online nodes.
	node1 := store.Node{
		ID:       "node-1",
		LastSeen: time.Now(),
	}
	node2 := store.Node{
		ID:       "node-2",
		LastSeen: time.Now(),
	}

	// Adds the first online node to the node store.
	if err := nodes.Upsert(ctx, node1); err != nil {
		t.Fatalf("adding node 1: %v", err)
	}

	// Adds the second online node to the node store.
	if err := nodes.Upsert(ctx, node2); err != nil {
		t.Fatalf("adding node 2: %v", err)
	}

	// Creation of three queued jobs.
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

	// Adds the queued jobs to the job store.
	for _, job := range []store.Job{job1, job2, job3} {
		if err := jobs.Create(ctx, job); err != nil {
			t.Fatalf("adding job %s: %v", job.ID, err)
		}
	}

	// Creates a new scheduler with the previously created node
	// and job stores.
	s := New(nodes, jobs)

	// Calls Tick() to attempt job assignment.
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("Tick() error: %v", err)
	}

	// Retrieves the list of jobs from the job store to verify
	// job distribution.
	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	// Testing whether exactly three jobs were retrieved.
	if len(got) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(got))
	}

	// Stores the number of jobs assigned to each node.
	nodeJobCount := make(map[string]int)

	// Iterates over the retrieved jobs to count the number
	// of jobs assigned to each node.
	for _, job := range got {
		if job.Status != store.JobAssigned {
			t.Errorf(
				"job %s: expected status %q, got %q",
				job.ID,
				store.JobAssigned,
				job.Status,
			)
		}

		nodeJobCount[job.NodeID]++
	}

	// Verifies that the job distribution between the two nodes is as expected.
	node1Count := nodeJobCount[node1.ID]
	node2Count := nodeJobCount[node2.ID]
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
// The job's status and NodeID should remain unchanged.
func TestNoJobsQueued(t *testing.T) {
	ctx := context.Background()

	nodes := store.NewMem()
	jobs := store.NewMemJobs()

	// Creation of an online node.
	node := store.Node{
		ID:       "node-1",
		LastSeen: time.Now(),
	}

	// Creation of a job that is already assigned to the node.
	job := store.Job{
		ID:     "job-1",
		Status: store.JobAssigned,
		NodeID: "node-1",
	}

	// Adds the node to the node store.
	if err := nodes.Upsert(ctx, node); err != nil {
		t.Fatalf("adding node: %v", err)
	}

	// Adds the job to the job store.
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("adding job: %v", err)
	}

	// Creates a new scheduler with the previously created node
	// and job stores.
	s := New(nodes, jobs)

	// Calls Tick() to attempt job assignment.
	if err := s.Tick(ctx, time.Now()); err != nil {
		t.Fatalf("expected Tick() to return nil, got %v", err)
	}

	// Retrieves the list of jobs from the job store to verify
	// that no changes have been made.
	got, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}

	// Testing whether exactly one job is present in the job store.
	if len(got) != 1 {
		t.Fatalf("expected 1 job, got %d", len(got))
	}

	// Testing whether the job's status remains assigned and
	// the NodeID remains unchanged.
	if got[0].Status != store.JobAssigned {
		t.Errorf(
			"expected status to remain %q, got %q",
			store.JobAssigned,
			got[0].Status,
		)
	}

	// Testing whether the job's NodeID remains unchanged.
	if got[0].NodeID != "node-1" {
		t.Errorf(
			"expected NodeID to remain %q, got %q",
			"node-1",
			got[0].NodeID,
		)
	}
}
