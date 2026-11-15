package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// TestBackupLiveDatabase checks that POST /admin/backup writes an online
// snapshot of a live SQLite database, returns it as a database file, and
// that reopening the snapshot preserves the pool, job, and credit state.
func TestBackupLiveDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "relay.db")
	backupPath := filepath.Join(dir, "backup.db")

	db, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer db.Close()

	pools, err := store.NewSQLitePools(db.DB())
	if err != nil {
		t.Fatalf("new sqlite pools: %v", err)
	}

	jobs, err := store.NewSQLiteJobs(db.DB())
	if err != nil {
		t.Fatalf("new sqlite jobs: %v", err)
	}

	credits, err := store.NewSQLiteCredits(db.DB())
	if err != nil {
		t.Fatalf("new sqlite credits: %v", err)
	}

	var pool store.Pool = store.Pool{
		ID:        "pool-1",
		Name:      "lab",
		Token:     "test-token",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := pools.Create(ctx, pool); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if err := pools.SetAdminTokenHash(ctx, pool.ID, store.HashToken("server-admin")); err != nil {
		t.Fatalf("set pool admin token: %v", err)
	}

	var job store.Job = store.Job{
		ID:        "job-1",
		Command:   []string{"true"},
		Status:    store.JobQueued,
		PoolID:    pool.ID,
		Submitter: "alice",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := jobs.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}

	var credit store.CreditEntry = store.CreditEntry{
		ID:        "credit-1",
		PoolID:    pool.ID,
		Account:   "alice",
		Delta:     5,
		Reason:    "earned",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := credits.Add(ctx, credit); err != nil {
		t.Fatalf("add credit: %v", err)
	}

	handler := NewHandler(store.NewMem(), pools, jobs, credits, testServer(), WithDatabase(db))

	req := httptest.NewRequest(http.MethodPost, "/admin/backup", nil)
	req.Header.Set("Authorization", "Bearer "+testServerToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("expected application/octet-stream, got %q", rec.Header().Get("Content-Type"))
	}

	var body []byte = rec.Body.Bytes()
	var wanted []byte = []byte("SQLite format 3")
	if len(body) < len(wanted) || !slices.Equal(body[:len(wanted)], wanted) {
		t.Fatalf("backup body does not start with a SQLite header")
	}

	if err := os.WriteFile(backupPath, body, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}

	db2, err := store.NewSQLite(backupPath)
	if err != nil {
		t.Fatalf("reopen backup: %v", err)
	}
	defer db2.Close()

	pools2, err := store.NewSQLitePools(db2.DB())
	if err != nil {
		t.Fatalf("reopen sqlite pools: %v", err)
	}

	jobs2, err := store.NewSQLiteJobs(db2.DB())
	if err != nil {
		t.Fatalf("reopen sqlite jobs: %v", err)
	}

	credits2, err := store.NewSQLiteCredits(db2.DB())
	if err != nil {
		t.Fatalf("reopen sqlite credits: %v", err)
	}

	gotPool, err := pools2.Get(ctx, pool.ID)
	if err != nil {
		t.Fatalf("read backed-up pool: %v", err)
	}
	if gotPool.Name != pool.Name || gotPool.Token != pool.Token {
		t.Fatalf("pool changed: name %q, token %q", gotPool.Name, gotPool.Token)
	}

	gotJob, err := jobs2.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("read backed-up job: %v", err)
	}
	if gotJob.Status != job.Status || gotJob.PoolID != job.PoolID || gotJob.Submitter != job.Submitter || gotJob.TimeoutSeconds != job.TimeoutSeconds || !slices.Equal(gotJob.Command, job.Command) {
		t.Fatalf("job changed after backup")
	}

	original, err := credits.Balances(ctx, pool.ID)
	if err != nil {
		t.Fatalf("read original balances: %v", err)
	}
	restored, err := credits2.Balances(ctx, pool.ID)
	if err != nil {
		t.Fatalf("read restored balances: %v", err)
	}
	if original["alice"] != 5 || restored["alice"] != original["alice"] {
		t.Errorf("credit balance changed: original %d, restored %d", original["alice"], restored["alice"])
	}
}

// TestBackupNoDatabase checks that a handler without a database answers
// 503 rather than offering an unusable backup endpoint.
func TestBackupNoDatabase(t *testing.T) {
	handler := NewHandler(store.NewMem(), store.NewMemPools(), store.NewMemJobs(),
		store.NewMemCredits(), testServer())

	req := httptest.NewRequest(http.MethodPost, "/admin/backup", nil)
	req.Header.Set("Authorization", "Bearer "+testServerToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "this scheduler cannot back up\n" {
		t.Fatalf("expected 503 message, got %q", rec.Body.String())
	}
}

// TestBackupRequiresAdmin checks that POST /admin/backup rejects a token
// that is not the server admin token.
func TestBackupRequiresAdmin(t *testing.T) {
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer db.Close()

	handler := NewHandler(store.NewMem(), store.NewMemPools(), store.NewMemJobs(),
		store.NewMemCredits(), testServer(), WithDatabase(db))

	req := httptest.NewRequest(http.MethodPost, "/admin/backup", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "invalid server admin token\n" {
		t.Fatalf("expected 401 message, got %q", rec.Body.String())
	}
}

type inspectingBackupWriter struct {
	*httptest.ResponseRecorder
	inspect func()
	fail    bool
}

func (w *inspectingBackupWriter) Write(p []byte) (int, error) {
	w.inspect()
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(p)
}

// The backup contains join tokens, so it must stay inside a private directory
// until streaming finishes, and be removed even if the client disconnects.
func TestBackupPrivateTemporaryDirectory(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "disconnected-client"
		}
		t.Run(name, func(t *testing.T) {
			db, err := store.NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			tmp := t.TempDir()
			for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(key, tmp)
			}
			inspected := false
			w := &inspectingBackupWriter{
				ResponseRecorder: httptest.NewRecorder(),
				fail:             fail,
				inspect: func() {
					inspected = true
					entries, err := os.ReadDir(tmp)
					if err != nil {
						t.Fatal(err)
					}
					if len(entries) != 1 || !entries[0].IsDir() {
						t.Fatalf("backup must be isolated in a directory, got %v", entries)
					}
					dir := filepath.Join(tmp, entries[0].Name())
					info, err := os.Stat(dir)
					if err != nil {
						t.Fatal(err)
					}
					if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
						t.Errorf("backup directory permissions = %o, want 700", info.Mode().Perm())
					}
					if _, err := os.Stat(filepath.Join(dir, "relay.db")); err != nil {
						t.Fatalf("backup file is missing while streaming: %v", err)
					}
				},
			}
			serveDatabaseBackup(w, httptest.NewRequest(http.MethodPost, "/admin/backup", nil), db)
			if !inspected {
				t.Fatalf("backup was not streamed: %s", w.Body.String())
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("temporary backup was not removed: %v", entries)
			}
		})
	}
}

func TestBackupFailureRemovesTemporaryDirectory(t *testing.T) {
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, tmp)
	}
	w := httptest.NewRecorder()
	serveDatabaseBackup(w, httptest.NewRequest(http.MethodPost, "/admin/backup", nil), db)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("failed backup left temporary files: %v", entries)
	}
}
