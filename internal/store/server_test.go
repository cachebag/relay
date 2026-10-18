package store

import (
	"context"
	"path/filepath"
	"testing"
)

// testServerStore is shared by the Mem and SQLite tests: no token at first,
// then the stored hash reads back and can be replaced.
func testServerStore(t *testing.T, s ServerStore) {
	t.Helper()
	ctx := context.Background()

	hash, err := s.AdminTokenHash(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if hash != "" {
		t.Fatalf("expected no token yet, got %q", hash)
	}

	if err := s.SetAdminTokenHash(ctx, "h1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if hash, _ = s.AdminTokenHash(ctx); hash != "h1" {
		t.Fatalf("expected h1, got %q", hash)
	}

	if err := s.SetAdminTokenHash(ctx, "h2"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if hash, _ = s.AdminTokenHash(ctx); hash != "h2" {
		t.Fatalf("expected h2 after replacing, got %q", hash)
	}
}

func TestMemServer(t *testing.T) {
	testServerStore(t, NewMemServer())
}

func TestSQLiteServer(t *testing.T) {
	db, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	s, err := NewSQLiteServer(db.DB())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	testServerStore(t, s)

	// The hash survives reopening the file.
	again, err := NewSQLiteServer(db.DB())
	if err != nil {
		t.Fatal(err)
	}
	if hash, _ := again.AdminTokenHash(context.Background()); hash != "h2" {
		t.Fatalf("expected h2 after reopen, got %q", hash)
	}
}
