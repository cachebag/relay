package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
