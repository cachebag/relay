package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func testPoolRoundTrip(t *testing.T, ps PoolStore) {
	t.Helper()
	ctx := context.Background()

	p := Pool{
		ID:        NewID(),
		Name:      "capstone",
		Token:     NewToken(),
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := ps.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != p.Name {
		t.Fatalf("want name %q, got %q", p.Name, got.Name)
	}
	if !got.CreatedAt.Equal(p.CreatedAt) {
		t.Fatalf("want created_at %v, got %v", p.CreatedAt, got.CreatedAt)
	}

	got, err = ps.GetByToken(ctx, p.Token)
	if err != nil {
		t.Fatalf("get by token: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("want id %q, got %q", p.ID, got.ID)
	}

	list, err := ps.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 pool, got %d", len(list))
	}
	if list[0].ID != p.ID {
		t.Fatalf("want id %q, got %q", p.ID, list[0].ID)
	}

	if _, err := ps.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for bogus id, got %v", err)
	}
	if _, err := ps.GetByToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for bogus token, got %v", err)
	}
}

func TestMemPools(t *testing.T) {
	testPoolRoundTrip(t, NewMemPools())
}

func TestNewToken(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) != 32 {
		t.Fatalf("want 32 chars, got %d", len(a))
	}
	if a == b {
		t.Fatal("tokens are not unique")
	}
}
