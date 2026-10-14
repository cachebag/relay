package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// TestEnsureAdminToken checks #96: the first start makes and prints a token,
// a second start doesn't, and --new-admin-token replaces it so the old one
// stops matching.
func TestEnsureAdminToken(t *testing.T) {
	ctx := context.Background()
	srv := store.NewMemServer()

	var out bytes.Buffer
	created, err := ensureAdminToken(ctx, srv, &out, false)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first start should make a token")
	}
	first := tokenFrom(t, out.String())
	hash, _ := srv.AdminTokenHash(ctx)
	if hash != store.HashToken(first) {
		t.Error("stored hash does not match the printed token")
	}
	if !strings.Contains(out.String(), "shown only once") {
		t.Errorf("expected a warning, got:\n%s", out.String())
	}

	out.Reset()
	created, err = ensureAdminToken(ctx, srv, &out, false)
	if err != nil {
		t.Fatal(err)
	}
	if created || out.Len() != 0 {
		t.Errorf("second start should keep the token and print nothing, got created=%v output=%q", created, out.String())
	}

	out.Reset()
	created, err = ensureAdminToken(ctx, srv, &out, true)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("--new-admin-token should make a token")
	}
	second := tokenFrom(t, out.String())
	hash, _ = srv.AdminTokenHash(ctx)
	if second == first || hash == store.HashToken(first) {
		t.Error("old token still matches after replacement")
	}
	if hash != store.HashToken(second) {
		t.Error("stored hash does not match the new token")
	}
}

// tokenFrom pulls the token out of ensureAdminToken's first output line.
func tokenFrom(t *testing.T, out string) string {
	t.Helper()
	line, _, _ := strings.Cut(out, "\n")
	_, token, ok := strings.Cut(line, "server admin token: ")
	if !ok || len(token) != 32 {
		t.Fatalf("expected a 32-character token line, got %q", line)
	}
	return token
}
