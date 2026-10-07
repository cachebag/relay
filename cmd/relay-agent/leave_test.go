package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

var testNode = store.Node{ID: "node-1", Hostname: "rafael"}

// statusServer answers every request with status.
func statusServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestLeaveSuccess checks that 204 and 404 both count as having left, that
// local data is cleared and each removed path is printed, and that the exit
// code is 0.
func TestLeaveSuccess(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := statusServer(t, status)
			cleared := false
			clear := func() ([]string, error) {
				cleared = true
				return []string{"/cache/relay/pending.json", "/cache/relay"}, nil
			}

			var out, errOut bytes.Buffer
			code := leave(context.Background(), srv.Client(), srv.URL, "tok", testNode, clear, &out, &errOut)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
			}
			if !cleared {
				t.Error("local data was not cleared")
			}
			for _, want := range []string{"rafael left the pool", "removed /cache/relay/pending.json", "removed /cache/relay"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("stdout missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

// TestLeaveFailures checks that a wrong token, an unreachable scheduler, and
// local data that can't be removed all exit 1, and that local data is kept
// when the machine didn't actually leave.
func TestLeaveFailures(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()

	cases := []struct {
		name        string
		url         string
		clearErr    error
		wantCleared bool
		wantStderr  string
	}{
		{"wrong token", statusServer(t, http.StatusForbidden).URL, nil, false, "doesn't match this machine's pool"},
		{"scheduler down", downURL, nil, false, "can't reach the scheduler"},
		{"server error", statusServer(t, http.StatusInternalServerError).URL, nil, false, "returned 500"},
		{"clear fails", statusServer(t, http.StatusNoContent).URL, errors.New("permission denied"), true, "couldn't remove"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cleared := false
			clear := func() ([]string, error) {
				cleared = true
				return nil, c.clearErr
			}

			var out, errOut bytes.Buffer
			code := leave(context.Background(), &http.Client{}, c.url, "tok", testNode, clear, &out, &errOut)

			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if cleared != c.wantCleared {
				t.Errorf("cleared = %v, want %v", cleared, c.wantCleared)
			}
			if !strings.Contains(errOut.String(), c.wantStderr) {
				t.Errorf("stderr missing %q:\n%s", c.wantStderr, errOut.String())
			}
		})
	}
}

// TestRunLeaveNeedsToken checks that leave refuses to run without a token.
func TestRunLeaveNeedsToken(t *testing.T) {
	t.Setenv("RELAY_POOL_TOKEN", "")

	var out, errOut bytes.Buffer
	if code := runLeave(nil, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "pool token is required") {
		t.Errorf("stderr = %q", errOut.String())
	}
}
