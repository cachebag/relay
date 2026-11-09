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

// TestRunStopsWhenRemoved verifies that a 410 heartbeat makes Run return nil
// right away, so relay-agent exits 0 instead of retrying, and that it prints
// why.
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

	if n := heartbeats.Load(); n != 1 {
		t.Errorf("expected exactly 1 heartbeat before stopping, got %d", n)
	}
	if !strings.Contains(logs.String(), "removed from this pool by the admin") {
		t.Errorf("expected the removal message, got log output:\n%s", logs.String())
	}
}

// TestRunRemovedStopsRunningJob verifies that a job still running when the
// machine is removed is stopped and not reported, since the scheduler has
// already put it back in the queue.
func TestRunRemovedStopsRunningJob(t *testing.T)
	{
		var gone atomic.Bool
		var results atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/nodes/heartbeat" && gone.Load():
					http.Error(w, "removed from pool by admin", http.StatusGone)
				case r.URL.Path == "/nodes/heartbeat":
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/nodes/n1/jobs/next":
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"id":"job1","command":["sleep"],"status":"running","node_id":"n1","attempts":1}`))
				case strings.HasSuffix(r.URL.Path, "/result"):
					results.Add(1)
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			},
		))
		defer srv.Close()

		a := newAgent(srv.URL, "tok", store.Node{ID: "n1", Hostname: "n1"}, t.TempDir())
		a.client = srv.Client()

		// A fake job that runs until it is cancelled. The admin removes the
		// machine once the job has started.
		started := make(chan struct{})
		a.run = func(ctx context.Context, argv []string, dir string) Result {
			close(started)
			gone.Store(true)
			<-ctx.Done()
			return Result{ExitCode: -1}
		}

		originalOutput := log.Writer()
		log.SetOutput(&bytes.Buffer{})
		defer log.SetOutput(originalOutput)

		done := make(chan error, 1)
		go func() { done <- a.Run(context.Background(), 10*time.Millisecond) }()

		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("job never started")
		}

		select {
		case err := <-done:
			if !errors.Is(err, ErrRemoved) {
				t.Fatalf("expected Run to return ErrRemoved so the agent exits 1, got %v", err)
			}
			if n := results.Load(); n != 0 {
				t.Errorf("expected no result to be reported for the requeued job, got %d", n)
			}
		}
		func TestRunStopsWhenTokenRejected(t *testing.T) {
		var heartbeats atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/nodes/heartbeat" {
					heartbeats.Add(1)
					http.Error(w, "invalid pool token", http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			},
		))
		defer srv.Close()

		a := newAgent(srv.URL, "bad", store.Node{ID: "n1", Hostname: "n1"}, t.TempDir())
		a.client = srv.Client()

		originalOutput := log.Writer()
		log.SetOutput(&bytes.Buffer{})
		defer log.SetOutput(originalOutput)

		done := make(chan error, 1)
		go func() { done <- a.Run(context.Background(), 10*time.Millisecond) }()

		select {
		case err := <-done:
			if !errors.Is(err, ErrBadToken) {
				t.Fatalf("expected Run to return ErrBadToken, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Run kept going after the scheduler rejected the pool token")
		}

		if n := heartbeats.Load(); n != 1 {
			t.Errorf("expected exactly 1 heartbeat before stopping, got %d", n)
		}
	}

	}