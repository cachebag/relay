package agent

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// TestNextAndReportSendPoolToken checks #94 on the agent side: both the
// jobs/next request and the result report carry the pool token as a bearer
// token.
func TestNextAndReportSendPoolToken(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/jobs/next") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"job1","command":["true"],"status":"assigned","node_id":"n1","attempts":1}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	a := newAgent(srv.URL, "secret-token", store.Node{ID: "n1", Hostname: "n1"}, t.TempDir())
	a.client = srv.Client()

	if _, err := a.Next(context.Background()); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if err := a.Report(context.Background(), store.Job{ID: "job1"}, resultBody{NodeID: "n1", Attempt: 1}, ""); err != nil {
		t.Fatalf("Report: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/nodes/n1/jobs/next", "/jobs/job1/result"} {
		if seen[path] != "Bearer secret-token" {
			t.Errorf("%s: expected bearer token, got %q", path, seen[path])
		}
	}
}

// TestNextAndReportRejectedToken checks that a 403 on either request comes
// back as ErrBadToken, so the agent logs it the same way as a rejected
// heartbeat, and that a rejected result is kept for a retry.
func TestNextAndReportRejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid pool token", http.StatusForbidden)
	}))
	defer srv.Close()

	a := newAgent(srv.URL, "bad", store.Node{ID: "n1", Hostname: "n1"}, t.TempDir())
	a.client = srv.Client()

	if _, err := a.Next(context.Background()); !errors.Is(err, ErrBadToken) {
		t.Errorf("Next: expected ErrBadToken, got %v", err)
	}
	if err := a.Report(context.Background(), store.Job{ID: "job1"}, resultBody{}, ""); !errors.Is(err, ErrBadToken) {
		t.Errorf("Report: expected ErrBadToken, got %v", err)
	}

	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(original)

	a.setPending(pending{JobID: "job1", Attempt: 1})
	if a.flushPending(context.Background()) {
		t.Error("a rejected token should not clear the pending result")
	}
	if a.pending == nil {
		t.Error("pending result was dropped")
	}
	if !strings.Contains(logs.String(), "pool token rejected") {
		t.Errorf("expected a token message, got:\n%s", logs.String())
	}
}
