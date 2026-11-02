package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func checkTimeoutStore(t *testing.T, jobs JobStore) {
	t.Helper()
	ctx := context.Background()

	job := Job{
		ID:             "timeout-test",
		Command:        []string{"echo", "hello"},
		TimeoutSeconds: 600,
		Status:         JobQueued,
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
	}

	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}

	if got.TimeoutSeconds != 600 {
		t.Fatalf("timeout = %d, want 600", got.TimeoutSeconds)
	}

	// Check that updating the job preserves the new timeout.
	got.TimeoutSeconds = 1200
	got.Status = JobAssigned

	if err := jobs.Update(ctx, got); err != nil {
		t.Fatalf("update job: %v", err)
	}

	updated, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get updated job: %v", err)
	}

	if updated.TimeoutSeconds != 1200 {
		t.Fatalf("updated timeout = %d, want 1200", updated.TimeoutSeconds)
	}

	// Check that List also returns the saved timeout.
	listed, err := jobs.List(ctx)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}

	if len(listed) != 1 {
		t.Fatalf("got %d jobs, want 1", len(listed))
	}

	if listed[0].TimeoutSeconds != 1200 {
		t.Fatalf("listed timeout = %d, want 1200", listed[0].TimeoutSeconds)
	}
}

func TestTimeoutStoresRoundTrip(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		checkTimeoutStore(t, NewMemJobs())
	})

	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "relay.db")

		db, err := NewSQLite(path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()

		jobs, err := NewSQLiteJobs(db.DB())
		if err != nil {
			t.Fatal(err)
		}

		checkTimeoutStore(t, jobs)
	})
}

func TestTimeoutSQLitePersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "relay.db")

	db, err := NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}

	jobs, err := NewSQLiteJobs(db.DB())
	if err != nil {
		db.Close()
		t.Fatal(err)
	}

	job := Job{
		ID:             "persistent-timeout",
		Command:        []string{"echo", "hello"},
		TimeoutSeconds: 300,
		Status:         JobQueued,
		CreatedAt:      time.Now().UTC(),
	}

	if err := jobs.Create(ctx, job); err != nil {
		db.Close()
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate restarting relayd.
	db, err = NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	jobs, err = NewSQLiteJobs(db.DB())
	if err != nil {
		t.Fatal(err)
	}

	got, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.TimeoutSeconds != 300 {
		t.Fatalf("timeout after restart = %d, want 300", got.TimeoutSeconds)
	}
}

func TestTimeoutLegacySQLiteMigration(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create a jobs table that predates timeout support.
	_, err = db.DB().Exec(`CREATE TABLE jobs (
		id TEXT PRIMARY KEY,
		command TEXT NOT NULL,
		status TEXT NOT NULL,
		node_id TEXT NOT NULL,
		attempts INTEGER NOT NULL,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.DB().Exec(`
		INSERT INTO jobs (id, command, status, node_id, attempts, created_at)
		VALUES ('legacy-job', '["echo","hello"]', 'queued', '', 0, 1)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Migration should be safe to run more than once.
	for i := 0; i < 2; i++ {
		jobs, err := NewSQLiteJobs(db.DB())
		if err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}

		got, err := jobs.Get(context.Background(), "legacy-job")
		if err != nil {
			t.Fatal(err)
		}

		if got.TimeoutSeconds != DefaultJobTimeoutSeconds {
			t.Fatalf(
				"legacy timeout = %d, want %d",
				got.TimeoutSeconds,
				DefaultJobTimeoutSeconds,
			)
		}
	}
}
