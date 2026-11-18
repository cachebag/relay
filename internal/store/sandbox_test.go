package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteKeepsANodesSandboxFlag(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, n := range []Node{
		{ID: "a", Hostname: "a", PoolID: "p", LastSeen: time.Now(), Sandbox: true},
		{ID: "b", Hostname: "b", PoolID: "p", LastSeen: time.Now()},
	} {
		if err := db.Upsert(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := db.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || !nodes[0].Sandbox || nodes[1].Sandbox {
		t.Fatalf("sandbox flags = %+v", nodes)
	}
}
