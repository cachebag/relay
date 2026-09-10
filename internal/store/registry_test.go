package store

import (
	"context"
	"testing"
	"time"
)

func testRoundTrip(t *testing.T, reg Registry) {
	t.Helper()
	ctx := context.Background()

	n := Node{ID: "node-a", Hostname: "node-a", OS: "linux", Arch: "amd64", CPUs: 4, MemBytes: 8_000_000_000, LastSeen: time.Now().UTC().Truncate(time.Second)}
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
	if got[0].Hostname != "node-a" || got[0].CPUs != 4 {
		t.Fatalf("unexpected node: %+v", got[0])
	}

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

func TestMem(t *testing.T) {
	testRoundTrip(t, NewMem())
}
