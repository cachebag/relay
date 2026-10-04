package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestSQLitePersistence verifies that pools and jobs remain available after
// the SQLite database is closed and reopened.
func TestSQLitePersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "relay.db")

	// Open the database for the first time.
	db, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}

	pools, err := NewSQLitePools(db.DB())
	if err != nil {
		db.Close()
		t.Fatalf("new sqlite pools: %v", err)
	}

	jobs, err := NewSQLiteJobs(db.DB())
	if err != nil {
		db.Close()
		t.Fatalf("new sqlite jobs: %v", err)
	}

	pool := Pool{
		ID:        NewID(),
		Name:      "restart-test",
		Token:     NewToken(),
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := pools.Create(ctx, pool); err != nil {
		db.Close()
		t.Fatalf("create pool: %v", err)
	}

	job := Job{
		ID:        NewID(),
		Command:   []string{"echo", "persistent"},
		Status:    JobQueued,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := jobs.Create(ctx, job); err != nil {
		db.Close()
		t.Fatalf("create job: %v", err)
	}

	// Simulate relayd shutting down.
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	// Reopen the exact same database file, simulating a relayd restart.
	db, err = NewSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer db.Close()

	pools, err = NewSQLitePools(db.DB())
	if err != nil {
		t.Fatalf("reopen sqlite pools: %v", err)
	}

	jobs, err = NewSQLiteJobs(db.DB())
	if err != nil {
		t.Fatalf("reopen sqlite jobs: %v", err)
	}

	gotPool, err := pools.Get(ctx, pool.ID)
	if err != nil {
		t.Fatalf("get pool after restart: %v", err)
	}

	if gotPool.ID != pool.ID {
		t.Fatalf("want pool id %q, got %q", pool.ID, gotPool.ID)
	}

	if gotPool.Token != pool.Token {
		t.Fatalf("want pool token %q, got %q", pool.Token, gotPool.Token)
	}

	gotJob, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job after restart: %v", err)
	}

	if gotJob.ID != job.ID {
		t.Fatalf("want job id %q, got %q", job.ID, gotJob.ID)
	}

	if gotJob.Status != job.Status {
		t.Fatalf("want job status %q, got %q", job.Status, gotJob.Status)
	}
}
