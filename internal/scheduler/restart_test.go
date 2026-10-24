package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// A scheduler restart must not requeue active jobs just because stored
// heartbeats are stale. This also protects jobs whose node is missing.
func TestStartupGracePeriodPreservesActiveJobs(t *testing.T) {
	cases := []struct {
		name        string
		status      store.JobStatus
		attempts    int
		includeNode bool
	}{
		{"running-stale-node", store.JobRunning, 1, true},
		{"assigned-stale-node", store.JobAssigned, 1, true},
		{"running-missing-node", store.JobRunning, 1, false},
		{"assigned-at-attempt-limit", store.JobAssigned, maxAttempts, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			reg := store.NewMem()
			jobs := store.NewMemJobs()

			if tc.includeNode {
				node := store.Node{
					ID: "node-1", PoolID: "pool-a",
					LastSeen: now.Add(-2 * store.NodeTimeout),
				}
				if err := reg.Upsert(ctx, node); err != nil {
					t.Fatal(err)
				}
			}

			job := store.Job{
				ID: "job-1", PoolID: "pool-a", Status: tc.status,
				NodeID: "node-1", Attempts: tc.attempts,
			}
			if err := jobs.Create(ctx, job); err != nil {
				t.Fatal(err)
			}

			s := New(reg, jobs, nil)
			s.started = now

			if err := s.Tick(ctx, now.Add(store.NodeTimeout-time.Second)); err != nil {
				t.Fatal(err)
			}

			got, err := jobs.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != job.Status || got.NodeID != job.NodeID || got.Attempts != job.Attempts {
				t.Errorf("job changed during grace period: status=%s node=%s attempts=%d",
					got.Status, got.NodeID, got.Attempts)
			}
		})
	}
}

// A heartbeat received after restart keeps the original job assignment
// alive when the grace period ends. The completed result stays intact.
func TestStartupHeartbeatPreservesOriginalResult(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	reg := store.NewMem()
	jobs := store.NewMemJobs()

	node := store.Node{
		ID: "node-1", PoolID: "pool-a",
		LastSeen: now.Add(-2 * store.NodeTimeout),
	}
	if err := reg.Upsert(ctx, node); err != nil {
		t.Fatal(err)
	}

	job := store.Job{
		ID: "job-1", PoolID: "pool-a", Status: store.JobRunning,
		NodeID: node.ID, Attempts: 1,
	}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}

	s := New(reg, jobs, nil)
	s.started = now

	if err := s.Tick(ctx, now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}

	// The agent heartbeats before the grace period expires.
	node.LastSeen = now.Add(25 * time.Second)
	if err := reg.Upsert(ctx, node); err != nil {
		t.Fatal(err)
	}

	if err := s.Tick(ctx, now.Add(store.NodeTimeout)); err != nil {
		t.Fatal(err)
	}

	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.JobRunning || got.NodeID != node.ID || got.Attempts != 1 {
		t.Fatalf("original assignment lost: status=%s node=%s attempts=%d",
			got.Status, got.NodeID, got.Attempts)
	}

	// Simulate accepting the agent's original completion.
	finished := now.Add(store.NodeTimeout + time.Second)
	got.Status = store.JobDone
	got.FinishedAt = &finished
	got.Stdout = "completed once"

	if err := jobs.Update(ctx, got); err != nil {
		t.Fatal(err)
	}

	if err := s.Tick(ctx, now.Add(2*store.NodeTimeout)); err != nil {
		t.Fatal(err)
	}

	got, err = jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.JobDone || got.NodeID != node.ID || got.Attempts != 1 ||
		got.FinishedAt == nil || got.Stdout != "completed once" {
		t.Errorf("completed job changed: status=%s node=%s attempts=%d finished=%v stdout=%q",
			got.Status, got.NodeID, got.Attempts, got.FinishedAt, got.Stdout)
	}
}

// At exactly NodeTimeout after startup, genuinely lost jobs can be recovered.
func TestStartupGracePeriodExpires(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "stale-node"
		if missing {
			name = "missing-node"
		}

		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			reg := store.NewMem()
			jobs := store.NewMemJobs()

			if !missing {
				if err := reg.Upsert(ctx, store.Node{
					ID: "node-1", PoolID: "pool-a",
					LastSeen: now.Add(-2 * store.NodeTimeout),
				}); err != nil {
					t.Fatal(err)
				}
			}

			job := store.Job{
				ID: "job-1", PoolID: "pool-a", Status: store.JobRunning,
				NodeID: "node-1", Attempts: 1,
			}
			if err := jobs.Create(ctx, job); err != nil {
				t.Fatal(err)
			}

			s := New(reg, jobs, nil)
			s.started = now

			if err := s.Tick(ctx, now.Add(store.NodeTimeout)); err != nil {
				t.Fatal(err)
			}

			got, err := jobs.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != store.JobQueued || got.NodeID != "" || got.Attempts != 1 {
				t.Errorf("expected queued with 1 attempt, got status=%s node=%s attempts=%d",
					got.Status, got.NodeID, got.Attempts)
			}
		})
	}
}

// Startup recovery protection must not delay placement of new queued jobs.
func TestStartupGracePeriodStillAssignsQueuedJobs(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	reg := store.NewMem()
	jobs := store.NewMemJobs()

	node := store.Node{ID: "node-1", PoolID: "pool-a", LastSeen: now}
	if err := reg.Upsert(ctx, node); err != nil {
		t.Fatal(err)
	}

	job := store.Job{ID: "job-1", PoolID: "pool-a", Status: store.JobQueued}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}

	s := New(reg, jobs, nil)
	s.started = now

	if err := s.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}

	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.JobAssigned || got.NodeID != node.ID || got.Attempts != 1 {
		t.Errorf("expected immediate assignment, got status=%s node=%s attempts=%d",
			got.Status, got.NodeID, got.Attempts)
	}
}
