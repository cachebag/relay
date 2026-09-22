package store

import (
	"context"
	"errors"
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
	if !got.CreatedAt.Equal(j.CreatedAt) {
		t.Fatalf("want created_at %v, got %v", j.CreatedAt, got.CreatedAt)
	}

	if err := js.Create(ctx, j); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists for duplicate id, got %v", err)
	}

	j.Status = JobAssigned
	j.NodeID = "node-a"
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
	if got.NodeID != "node-a" {
		t.Fatalf("want node_id %q, got %q", "node-a", got.NodeID)
	}

	list, err := js.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 job, got %d", len(list))
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
