package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// testRoundTrip is a conformance suite for the Registry interface, not a test of
// Mem specifically. Each backend gets a thin TestXxx wrapper that hands it a
// fresh instance (see TestMem below; SQLite joins in #11), so every
// implementation is held to the same behavior.
func testRoundTrip(t *testing.T, reg Registry) {
	t.Helper()
	ctx := context.Background()

	// Truncated to the second because backends that round-trip timestamps through
	// storage may not preserve sub-second precision.
	n := Node{ID: "node-a", PoolID: "pool-a", Hostname: "node-a", OS: "linux", Arch: "amd64", CPUs: 4, MemBytes: 8_000_000_000, LastSeen: time.Now().UTC().Truncate(time.Second)}
	if err := reg.Upsert(ctx, n); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 node, got %d", len(got))
	}
	if got[0].Hostname != "node-a" || got[0].CPUs != 4 || got[0].PoolID != "pool-a" {
		t.Fatalf("unexpected node: %+v", got[0])
	}

	// Re-Upsert the same ID with changed data: this is the "update" half of upsert.
	// The count staying at 1 is the real assertion — a backend that inserted
	// blindly would leave two rows here.
	n.CPUs = 8
	if err := reg.Upsert(ctx, n); err != nil {
		t.Fatalf("upsert again: %v", err)
	}
	got, err = reg.List(ctx)
	if err != nil {
		t.Fatalf("list again: %v", err)
	}
	if len(got) != 1 || got[0].CPUs != 8 {
		t.Fatalf("want 1 node with 8 cpus, got %+v", got)
	}
}

// testDelete checks that Delete removes only the named node and reports
// ErrNotFound for an ID the registry doesn't have.
func testDelete(t *testing.T, reg Registry) {
	t.Helper()
	ctx := context.Background()

	for _, id := range []string{"node-a", "node-b"} {
		if err := reg.Upsert(ctx, Node{ID: id, Hostname: id, OS: "linux", Arch: "amd64", CPUs: 1, MemBytes: 1}); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	if err := reg.Delete(ctx, "node-a"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got, err := reg.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "node-b" {
		t.Fatalf("want only node-b left, got %+v", got)
	}

	if err := reg.Delete(ctx, "node-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete again: want ErrNotFound, got %v", err)
	}
}

func TestMem(t *testing.T) {
	testRoundTrip(t, NewMem())
	testDelete(t, NewMem())
}

func TestSQLite(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer db.Close() // Windows locks open files, so TempDir cleanup fails without this.
	testRoundTrip(t, db)

	db2, err := NewSQLite(filepath.Join(t.TempDir(), "delete.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer db2.Close()
	testDelete(t, db2)
}
