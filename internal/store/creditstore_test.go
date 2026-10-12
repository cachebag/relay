package store

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"testing"
	"time"
)

// testCreditBalances is a conformance suite for CreditStore: entries for two
// accounts across two pools, checked per pool on every backend.
func testCreditBalances(t *testing.T, cs CreditStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entries := []CreditEntry{
		{ID: "e1", PoolID: "pool-a", Account: "akrm", Delta: 10, Reason: "online", CreatedAt: now},
		{ID: "e2", PoolID: "pool-a", Account: "akrm", Delta: 5, Reason: "online", CreatedAt: now},
		{ID: "e3", PoolID: "pool-a", Account: "omar", Delta: 10, Reason: "online", CreatedAt: now},
		{ID: "e4", PoolID: "pool-a", Account: "omar", Delta: -3, Reason: "job", JobID: "job-1", CreatedAt: now},
		{ID: "e5", PoolID: "pool-b", Account: "akrm", Delta: 7, Reason: "online", CreatedAt: now},
	}
	for _, e := range entries {
		if err := cs.Add(ctx, e); err != nil {
			t.Fatalf("add %s: %v", e.ID, err)
		}
	}

	if err := cs.Add(ctx, entries[0]); !errors.Is(err, ErrExists) {
		t.Fatalf("add duplicate: want ErrExists, got %v", err)
	}

	for pool, want := range map[string]map[string]int{
		"pool-a": {"akrm": 15, "omar": 7},
		"pool-b": {"akrm": 7},
		"pool-c": {},
	} {
		got, err := cs.Balances(ctx, pool)
		if err != nil {
			t.Fatalf("balances %s: %v", pool, err)
		}
		if !maps.Equal(got, want) {
			t.Errorf("balances %s: want %v, got %v", pool, want, got)
		}
	}
}

func TestMemCredits(t *testing.T) {
	testCreditBalances(t, NewMemCredits())
}

func TestSQLiteCredits(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer db.Close() // Windows locks open files, so TempDir cleanup fails without this.

	cs, err := NewSQLiteCredits(db.DB())
	if err != nil {
		t.Fatalf("NewSQLiteCredits: %v", err)
	}
	testCreditBalances(t, cs)
}
