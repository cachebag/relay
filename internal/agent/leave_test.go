package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestLeaveSendsDelete checks the request Leave sends: a DELETE for this
// node's (escaped) ID with the join token as a bearer token.
func TestLeaveSendsDelete(t *testing.T) {
	var method, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Leave(context.Background(), srv.Client(), srv.URL, "my host", "tok"); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", method)
	}
	if path != "/nodes/my%20host" {
		t.Errorf("path = %s, want /nodes/my%%20host", path)
	}
	if auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer tok")
	}
}

// TestLeaveResponses checks how each scheduler answer is treated: 204 and 404
// are success, 403 is ErrNotInPool, and anything else is a plain error.
func TestLeaveResponses(t *testing.T) {
	cases := []struct {
		status    int
		wantErr   bool
		notInPool bool
	}{
		{http.StatusNoContent, false, false},
		{http.StatusNotFound, false, false},
		{http.StatusForbidden, true, true},
		{http.StatusInternalServerError, true, false},
	}
	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
			}))
			defer srv.Close()

			err := Leave(context.Background(), srv.Client(), srv.URL, "node", "tok")
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if errors.Is(err, ErrNotInPool) != c.notInPool {
				t.Errorf("errors.Is(err, ErrNotInPool) = %v, want %v", errors.Is(err, ErrNotInPool), c.notInPool)
			}
		})
	}
}

// TestLeaveUnreachable checks that a scheduler that can't be reached gives an
// error that isn't mistaken for a wrong token.
func TestLeaveUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := Leave(context.Background(), &http.Client{}, url, "node", "tok")
	if err == nil {
		t.Fatal("expected an error for an unreachable scheduler")
	}
	if errors.Is(err, ErrNotInPool) {
		t.Errorf("unreachable scheduler reported as ErrNotInPool: %v", err)
	}
}

// TestClearLocalData checks that the pending result file is deleted, that the
// cache directory goes with it only when nothing else is in it, and that a
// missing directory is not an error.
func TestClearLocalData(t *testing.T) {
	t.Run("removes pending file and empty dir", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "relay")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		pending := filepath.Join(dir, pendingFile)
		if err := os.WriteFile(pending, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}

		removed, err := clearLocalData(dir)
		if err != nil {
			t.Fatalf("clearLocalData: %v", err)
		}
		if !slices.Equal(removed, []string{pending, dir}) {
			t.Errorf("removed = %v, want [%s %s]", removed, pending, dir)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %s to be gone, stat err = %v", dir, err)
		}
	})

	t.Run("keeps dir with other files", func(t *testing.T) {
		dir := t.TempDir()
		pending := filepath.Join(dir, pendingFile)
		other := filepath.Join(dir, "other")
		for _, p := range []string{pending, other} {
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		removed, err := clearLocalData(dir)
		if err != nil {
			t.Fatalf("clearLocalData: %v", err)
		}
		if !slices.Equal(removed, []string{pending}) {
			t.Errorf("removed = %v, want [%s]", removed, pending)
		}
		if _, err := os.Stat(other); err != nil {
			t.Errorf("expected %s to stay: %v", other, err)
		}
	})

	t.Run("missing dir", func(t *testing.T) {
		removed, err := clearLocalData(filepath.Join(t.TempDir(), "nope"))
		if err != nil || len(removed) != 0 {
			t.Errorf("got removed=%v err=%v, want nothing", removed, err)
		}
	})
}
