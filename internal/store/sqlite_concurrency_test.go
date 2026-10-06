package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestSQLiteConcurrentAccess hammers one database the way relayd does:
// heartbeats upserting nodes while the scheduler lists nodes and updates
// jobs. Every call must succeed. Without WAL and a busy timeout, some fail
// straight away with SQLITE_BUSY ("database is locked").
func TestSQLiteConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	reg, err := NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	jobs, err := NewSQLiteJobs(reg.DB())
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "job-1", Command: []string{"true"}, Status: JobQueued, CreatedAt: time.Now().UTC()}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatal(err)
	}

	const workers, iterations = 8, 100
	errs := make(chan error, 3*workers*iterations)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(3)

		// Heartbeats.
		go func() {
			defer wg.Done()
			for i := range iterations {
				n := Node{
					ID:       fmt.Sprintf("node-%d", w),
					Hostname: fmt.Sprintf("node-%d", w),
					OS:       "linux",
					Arch:     "amd64",
					CPUs:     i + 1,
					MemBytes: 1,
					LastSeen: time.Now().UTC(),
				}
				if err := reg.Upsert(ctx, n); err != nil {
					errs <- fmt.Errorf("upsert: %w", err)
				}
			}
		}()

		// Scheduler reading nodes.
		go func() {
			defer wg.Done()
			for range iterations {
				if _, err := reg.List(ctx); err != nil {
					errs <- fmt.Errorf("list: %w", err)
				}
			}
		}()

		// Scheduler updating jobs.
		go func() {
			defer wg.Done()
			for i := range iterations {
				j := job
				j.Attempts = i
				if err := jobs.Update(ctx, j); err != nil {
					errs <- fmt.Errorf("update job: %w", err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if failed < 5 {
			t.Error(err)
		}
		failed++
	}
	if failed > 0 {
		t.Fatalf("%d of %d calls failed", failed, 3*workers*iterations)
	}

	nodes, err := reg.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != workers {
		t.Errorf("got %d nodes, want %d", len(nodes), workers)
	}
}

// TestSQLitePragmas checks that WAL and the busy timeout are in effect. The
// busy timeout is per connection, so it is checked on several connections
// from the pool, not just the first.
func TestSQLitePragmas(t *testing.T) {
	ctx := context.Background()
	reg, err := NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	for i := range 3 {
		conn, err := reg.DB().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		var mode string
		var timeout int
		if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if mode != "wal" {
			t.Errorf("connection %d: journal_mode = %q, want wal", i, mode)
		}
		if timeout != 5000 {
			t.Errorf("connection %d: busy_timeout = %d, want 5000", i, timeout)
		}
	}
}
