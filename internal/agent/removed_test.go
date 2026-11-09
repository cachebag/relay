package agent

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// TestSendRemoved verifies that a 410 Gone from the scheduler is returned as
// ErrRemoved, and is not mistaken for a rejected token.
func TestSendRemoved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "removed from pool by admin", http.StatusGone)
		},
	))
	defer server.Close()

	err := Send(context.Background(), server.Client(), server.URL, store.Node{ID: "n1"}, "tok")
	if !errors.Is(err, ErrRemoved) {
		t.Fatalf("expected ErrRemoved, got %v", err)
	}
	if errors.Is(err, ErrBadToken) {
		t.Fatalf("410 should not be reported as a bad token: %v", err)
	}
}

func TestRunStopsWhenRemoved(t *testing.T) {
	var heartbeats atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/nodes/heartbeat" {
				heartbeats.Add(1)
				http.Error(w, "removed from pool by admin", http.StatusGone)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		},
	))
	defer srv.Close()

	a := newAgent(srv.URL, "tok", store.Node{ID: "n1", Hostname: "n1"}, t.TempDir())
	a.client = srv.Client()

	var logs bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	done := make(chan error, 1)
	go func() { done <- a.Run(context.Background(), 10*time.Millisecond) }()

	select {
	case err := <-done:
		if !errors.Is(err, ErrRemoved) {
			t.Fatalf("expected Run to return ErrRemoved so the agent exits 1, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept going after the scheduler said the machine was removed")
	}

	if n := heartbeats.Load(); n != 1 {
		t.Errorf("expected exactly 1 heartbeat before stopping, got %d", n)
	}
	if !strings.Contains(logs.String(), "removed from this pool by the admin") {
		t.Errorf("expected the removal message, got log output:\n%s", logs.String())
	}
}
