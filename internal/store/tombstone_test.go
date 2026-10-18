package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// testTombstones is shared by the Mem and SQLite tests. It checks what the
// Tombstones interface promises: adding the same tombstone twice is fine,
// and a tombstone only matches its own pool and node.
func testTombstones(t *testing.T, ts Tombstones) {
	t.Helper()
	ctx := context.Background()

	has, err := ts.HasTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if has {
		t.Fatal("expected no tombstone before adding one")
	}

	tomb := Tombstone{PoolID: "p1", NodeID: "n1", CreatedAt: time.Now().UTC()}
	if err := ts.AddTombstone(ctx, tomb); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := ts.AddTombstone(ctx, tomb); err != nil {
		t.Fatalf("adding the same tombstone twice should succeed: %v", err)
	}

	has, err = ts.HasTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has {
		t.Fatal("expected a tombstone for p1/n1")
	}

	// A node removed from one pool can still join another, and another
	// node in the same pool is unaffected.
	for _, c := range []struct{ pool, node string }{{"p2", "n1"}, {"p1", "n2"}} {
		has, err := ts.HasTombstone(ctx, c.pool, c.node)
		if err != nil {
			t.Fatalf("has %s/%s: %v", c.pool, c.node, err)
		}
		if has {
			t.Fatalf("expected no tombstone for %s/%s", c.pool, c.node)
		}
	}
}

func TestMemTombstones(t *testing.T) {
	testTombstones(t, NewMem())
}

func TestSQLiteTombstones(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	testTombstones(t, db)
}
