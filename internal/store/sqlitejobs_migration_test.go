package store

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSQLiteJobsMigratesOwnership verifies that an existing pre-I79 database
// gains ownership columns without discarding its existing jobs.
func TestSQLiteJobsMigratesOwnership(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.DB().Exec(`CREATE TABLE jobs (
		id TEXT PRIMARY KEY,
		command TEXT NOT NULL,
		status TEXT NOT NULL,
		node_id TEXT NOT NULL,
		attempts INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		exit_code INTEGER NOT NULL DEFAULT 0,
		stdout TEXT NOT NULL DEFAULT '',
		stderr TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	_, err = db.DB().Exec(`INSERT INTO jobs (id, command, status, node_id, attempts, created_at)
		VALUES ('legacy-job', '["echo","hi"]', 'queued', '', 0, 1)`)
	if err != nil {
		t.Fatalf("insert legacy job: %v", err)
	}

	for i := 0; i < 2; i++ {
		jobs, err := NewSQLiteJobs(db.DB())
		if err != nil {
			t.Fatalf("migrate attempt %d: %v", i+1, err)
		}
		got, err := jobs.Get(context.Background(), "legacy-job")
		if err != nil {
			t.Fatalf("get legacy job: %v", err)
		}
		if got.PoolID != "" || got.Submitter != "" {
			t.Fatalf("legacy job ownership: pool=%q submitter=%q, want empty", got.PoolID, got.Submitter)
		}
		if got.Requires != (Requires{}) {
			t.Fatalf("legacy job requires = %+v, want empty", got.Requires)
		}
	}
}
