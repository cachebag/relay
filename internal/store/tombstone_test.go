package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// testTombstones is shared by the Mem and SQLite tests. It checks what the
// Tombstones interface promises: adding the same tombstone twice keeps one
// row and replaces its token hash, a tombstone only matches its own pool
// and node, and deleting a missing row succeeds.
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

	_, found, err := ts.GetTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if found {
		t.Fatal("expected no tombstone before adding one")
	}

	tomb := Tombstone{PoolID: "p1", NodeID: "n1", CreatedAt: time.Now().UTC(), TokenHash: "h1"}
	if err := ts.AddTombstone(ctx, tomb); err != nil {
		t.Fatalf("add: %v", err)
	}
	again := tomb
	again.TokenHash = "h2"
	again.CreatedAt = tomb.CreatedAt.Add(time.Hour)
	if err := ts.AddTombstone(ctx, again); err != nil {
		t.Fatalf("adding the same tombstone twice should succeed: %v", err)
	}

	has, err = ts.HasTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !has {
		t.Fatal("expected a tombstone for p1/n1")
	}
	got, found, err := ts.GetTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !found {
		t.Fatal("expected a tombstone for p1/n1")
	}
	if got.TokenHash != "h2" {
		t.Fatalf("token hash = %q, want h2", got.TokenHash)
	}
	// SQLite stores unix seconds, so compare at that resolution.
	if got.CreatedAt.Unix() != again.CreatedAt.Unix() {
		t.Fatalf("created at = %v, want %v", got.CreatedAt, again.CreatedAt)
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
		_, found, err := ts.GetTombstone(ctx, c.pool, c.node)
		if err != nil {
			t.Fatalf("get %s/%s: %v", c.pool, c.node, err)
		}
		if found {
			t.Fatalf("expected no tombstone for %s/%s", c.pool, c.node)
		}
	}

	if err := ts.DeleteTombstone(ctx, "p1", "n1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := ts.DeleteTombstone(ctx, "p1", "n1"); err != nil {
		t.Fatalf("deleting a missing tombstone should succeed: %v", err)
	}
	has, err = ts.HasTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if has {
		t.Fatal("expected tombstone to be gone")
	}
}

// TestSQLiteTombstoneTokenHashMigration checks that a SQLite database written before
// the token_hash column was added gains the column, and that empty token_hash
// values are correctly filled from the pool's current token without overwriting
// existing non-empty token_hash values.
func TestSQLiteTombstoneTokenHashMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = raw.Exec(`CREATE TABLE tombstones (
		pool_id    TEXT NOT NULL,
		node_id    TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		PRIMARY KEY (pool_id, node_id)
	)`)
	if err != nil {
		t.Fatalf("create old tombstones: %v", err)
	}
	_, err = raw.Exec(`CREATE TABLE pools (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		token      TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create pools: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO pools (id, name, token, created_at) VALUES ('p1', 'one', 'tok1', 1)`); err != nil {
		t.Fatalf("insert pool: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO tombstones (pool_id, node_id, created_at) VALUES ('p1', 'n1', 1), ('p1', 'n2', 1)`); err != nil {
		t.Fatalf("insert tombstones: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	got, found, err := db.GetTombstone(ctx, "p1", "n1")
	if err != nil || !found {
		t.Fatalf("get n1: found=%v err=%v", found, err)
	}
	if got.TokenHash != HashToken("tok1") {
		t.Fatalf("n1 hash = %q, want hash of tok1", got.TokenHash)
	}

	// A hash already stored must survive a later rotate of the pool token.
	if _, err := db.DB().Exec(`UPDATE tombstones SET token_hash = 'kept' WHERE node_id = 'n2'`); err != nil {
		t.Fatalf("set hash: %v", err)
	}
	if _, err := db.DB().Exec(`UPDATE pools SET token = 'tok2' WHERE id = 'p1'`); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err = NewSQLite(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db.Close()
	got, found, err = db.GetTombstone(ctx, "p1", "n2")
	if err != nil || !found {
		t.Fatalf("get n2: found=%v err=%v", found, err)
	}
	if got.TokenHash != "kept" {
		t.Fatalf("n2 hash = %q, want kept", got.TokenHash)
	}
	got, _, err = db.GetTombstone(ctx, "p1", "n1")
	if err != nil {
		t.Fatalf("get n1: %v", err)
	}
	if got.TokenHash != HashToken("tok1") {
		t.Fatalf("n1 hash changed to %q", got.TokenHash)
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
