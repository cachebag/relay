package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/agent"
	"github.com/cachebag/relay/internal/store"
)

func TestStartupSaysConnecting(t *testing.T) {
	var out bytes.Buffer
	printStartup(&out, store.Node{Hostname: "m", Owner: "akrm"}, "https://relay.example")

	if !strings.Contains(out.String(), "connecting to https://relay.example as akrm") {
		t.Errorf("startup line should say connecting to, got %q", out.String())
	}
	if strings.Contains(out.String(), "joining") {
		t.Errorf("startup line still says joining: %q", out.String())
	}
}

func TestRemovedAgentExitsCleanly(t *testing.T) {
	// Keep the node-id file out of the real user cache directory.
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	t.Setenv("LOCALAPPDATA", cache)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "removed from pool by admin", http.StatusGone)
	}))
	defer srv.Close()

	// Send log lines and the final line to the same buffer,
	// like the terminal or the selected agent's log file would see them.
	var out bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(originalOutput)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := agent.New(srv.URL, "tok", store.Node{Hostname: "n1"}).Run(ctx, 10*time.Millisecond)

	code, handled := reportExit(&out, err)
	if !handled || code != 0 {
		t.Fatalf("expected removal to exit 0, got code=%d handled=%v (Run returned %v)", code, handled, err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if last := lines[len(lines)-1]; last != agent.RemovedExitMsg {
		t.Errorf("last line = %q, want %q", last, agent.RemovedExitMsg)
	}
}

func TestReportExit(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		want    string
		code    int
		handled bool
	}{
		{"removed", fmt.Errorf("%w: scheduler returned 410", agent.ErrRemoved), agent.RemovedExitMsg + "\n", 0, true},
		{"rejected", fmt.Errorf("%w: scheduler returned 403", agent.ErrBadToken), agent.RejectedExitMsg + "\n", 1, true},
		{"other", errors.New("disk full"), "", 0, false},
		{"nil", nil, "", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			code, handled := reportExit(&out, c.err)
			if code != c.code || handled != c.handled {
				t.Errorf("reportExit = (%d, %v), want (%d, %v)", code, handled, c.code, c.handled)
			}
			if out.String() != c.want {
				t.Errorf("output = %q, want %q", out.String(), c.want)
			}
		})
	}
}
