package agent

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

func TestTickRotatedTokenPrintedOnce(t *testing.T) {
	var beats atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nodes/heartbeat" {
			beats.Add(1)
			http.Error(w, store.RotatedTokenMsg, http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	a := newAgent(srv.URL, "old-token", store.Node{ID: "test-node", Hostname: "test"}, t.TempDir())
	a.client = srv.Client()

	var logs bytes.Buffer
	origOut, origFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() { log.SetOutput(origOut); log.SetFlags(origFlags) }()

	a.Tick(context.Background())
	a.Tick(context.Background())

	if n := strings.Count(logs.String(), store.RotatedTokenMsg); n != 1 {
		t.Fatalf("rotated message printed %d times, want 1. Logs:\n%s", n, logs.String())
	}
	if beats.Load() != 2 {
		t.Fatalf("heartbeats sent = %d, want 2 (loop should keep going)", beats.Load())
	}
}
