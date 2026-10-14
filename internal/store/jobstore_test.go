package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func testJobRoundTrip(t *testing.T, js JobStore) {
	t.Helper()
	ctx := context.Background()

	j := Job{
		ID:        NewID(),
		Command:   []string{"echo", "hello"},
		Status:    JobQueued,
		PoolID:    "pool-a",
		Submitter: "alice",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := js.Create(ctx, j); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := js.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != j.ID {
		t.Fatalf("want id %q, got %q", j.ID, got.ID)
	}
	if !slices.Equal(got.Command, j.Command) {
		t.Fatalf("want command %v, got %v", j.Command, got.Command)
	}
	if got.Status != JobQueued {
		t.Fatalf("want status %q, got %q", JobQueued, got.Status)
	}
	if got.PoolID != j.PoolID || got.Submitter != j.Submitter {
		t.Fatalf("ownership after create: got pool=%q submitter=%q, want pool=%q submitter=%q", got.PoolID, got.Submitter, j.PoolID, j.Submitter)
	}
	if !got.CreatedAt.Equal(j.CreatedAt) {
		t.Fatalf("want created_at %v, got %v", j.CreatedAt, got.CreatedAt)
	}

	if err := js.Create(ctx, j); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists for duplicate id, got %v", err)
	}

	j.Status = JobAssigned
	j.PoolID = "pool-b"
	j.Submitter = "bob"
	j.NodeID = "node-a"
	j.ExitCode = 3
	j.Stdout = "hi\n"
	j.Stderr = "boom \u00e9\n"
	if err := js.Update(ctx, j); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err = js.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Status != JobAssigned {
		t.Fatalf("want status %q, got %q", JobAssigned, got.Status)
	}
	if got.PoolID != j.PoolID || got.Submitter != j.Submitter {
		t.Fatalf("ownership after update: got pool=%q submitter=%q, want pool=%q submitter=%q", got.PoolID, got.Submitter, j.PoolID, j.Submitter)
	}
	if got.NodeID != "node-a" {
		t.Fatalf("want node_id %q, got %q", "node-a", got.NodeID)
	}
	if got.ExitCode != 3 {
		t.Fatalf("want exit_code 3, got %d", got.ExitCode)
	}
	if got.Stdout != "hi\n" {
		t.Fatalf("want stdout %q, got %q", "hi\n", got.Stdout)
	}
	if got.Stderr != "boom \u00e9\n" {
		t.Fatalf("want stderr %q, got %q", "boom \u00e9\n", got.Stderr)
	}

	list, err := js.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 job, got %d", len(list))
	}
	if list[0].PoolID != j.PoolID || list[0].Submitter != j.Submitter {
		t.Fatalf("ownership in list: got pool=%q submitter=%q", list[0].PoolID, list[0].Submitter)
	}
	if list[0].ID != j.ID {
		t.Fatalf("want id %q, got %q", j.ID, list[0].ID)
	}

	if _, err := js.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	if err := js.Update(ctx, Job{ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound updating missing job, got %v", err)
	}
}

func TestMemJobs(t *testing.T) {
	testJobRoundTrip(t, NewMemJobs())
}

func TestSQLiteJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.db")

	db, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer db.Close()

	jobs, err := NewSQLiteJobs(db.DB())
	if err != nil {
		t.Fatalf("new sqlite jobs: %v", err)
	}

	testJobRoundTrip(t, jobs)
}
