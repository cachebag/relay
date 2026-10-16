package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// Verifies the Collect() function is storing information that is
// somewhat plausible.
// Asserts: Hostname != "", CPUs > 0, MemBytes > 0.
func TestCollect(t *testing.T) {

	// Testing Collect() function
	n, err := Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// Asserting plausiblity.
	if n.Hostname == "" {
		t.Error("Hostname still empty")
	}
	if n.ID == "" || n.ID == n.Hostname {
		t.Errorf("ID = %q, want a generated ID distinct from hostname %q", n.ID, n.Hostname)
	}

	if n.CPUs <= 0 {
		t.Errorf("Expected CPUs: > 0. Actual: %d", n.CPUs)
	}

	if n.MemBytes <= 0 {
		t.Errorf("Expected MemBytes: > 0. Actual: %d", n.MemBytes)
	}
}

// Verifies that the agent correctly persists its node ID
// to the cache directory and reuses it on subsequent instantiations. Also checks
// that the node ID is written to the node-id file and that a new agent created
// with the same cache directory retrieves the same ID.
func TestAgentPersistsNodeID(t *testing.T) {
	cacheDir := t.TempDir()
	node := store.Node{Hostname: "test-host"}

	first := newAgent("", "", node, cacheDir)
	if first.node.ID == "" {
		t.Fatal("first agent has an empty ID")
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, nodeIDFile))
	if err != nil {
		t.Fatalf("reading node ID file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != first.node.ID {
		t.Fatalf("node-id contains %q, want %q", got, first.node.ID)
	}

	second := newAgent("", "", node, cacheDir)
	if second.node.ID != first.node.ID {
		t.Errorf("second agent ID = %q, want persisted ID %q", second.node.ID, first.node.ID)
	}
}

func TestAgentUsesFreshNodeIDWithoutUsableCache(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(cacheDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newAgent("", "", store.Node{Hostname: "test-host"}, cacheDir)
	if len(a.node.ID) != 12 {
		t.Errorf("agent ID = %q, want a generated 12-character ID", a.node.ID)
	}
}

// Verifies the Send() function is functioning correctly. Does this by
// verifying whether the agent sends the correct Hostname in its heartbeat
// to the scheduler when the scheduler can be reached. Includes subtests
// that test for when the agent can successfully reach the scheduler,
// and for when it can not.
func TestSend(t *testing.T) {

	// Subtest for when the agent can successfully reach the scheduler.
	t.Run("204 response (success)", func(t *testing.T) {
		var receivedHeartbeat heartbeat

		// Creates temporary server with a handler that captures
		// the request body and responds with 204 (success).
		srv := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {

					// Decodes request body into heartbeat struct.
					err := json.NewDecoder(r.Body).Decode(&receivedHeartbeat)
					if err != nil {
						t.Errorf("Failed to decode request body: %v", err)
						return
					}

					// Responds with 204
					w.WriteHeader(http.StatusNoContent)
				}))

		// Stops server once current function exit,
		// preventing unneccesary resource consumption.
		defer srv.Close()

		node := store.Node{
			Hostname: "test",
		}

		token := "test-token"

		// Testing Send() function
		err := Send(context.Background(), srv.Client(), srv.URL, node, token)
		if err != nil {
			t.Fatalf("Send() error: %v", err)
		}

		// Testing whether heartbeat contains correct Hostname.
		if receivedHeartbeat.Hostname != node.Hostname {
			t.Errorf(
				"Expected Hostname: %q. Actual Hostname: %q",
				node.Hostname,
				receivedHeartbeat.Hostname,
			)
		}

		// Testing whether heartbeat contains correct pool token.
		if receivedHeartbeat.PoolToken != token {
			t.Errorf(
				"Expected pool_token: %q. Actual pool_token: %q",
				token,
				receivedHeartbeat.PoolToken,
			)
		}
	})

	// Subtest for when the agent can not reach the scheduler.
	t.Run("500 response (unsuccessful)", func(t *testing.T) {

		// Creates a temporary server that is unresponsive
		srv := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))

		// Stops server once current function exit,
		// preventing unneccesary resource consumption.
		defer srv.Close()

		node := store.Node{
			Hostname: "test",
		}

		token := "test-token"

		err := Send(
			context.Background(),
			srv.Client(),
			srv.URL,
			node,
			token,
		)
		if err == nil {
			t.Fatal(
				"Expected Send() to return an error due to the scheduler",
				"being unresponsive, got nil instead.")
		}

	})

	// Subtest for when scheduler rejects the pool token.
	t.Run("403 response (invalid token)", func(t *testing.T) {

		// Creates temporary server that always responds with 403.
		srv := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
				}))

		defer srv.Close()

		node := store.Node{
			Hostname: "test",
		}

		token := "reject-token"

		err := Send(
			context.Background(),
			srv.Client(),
			srv.URL,
			node,
			token,
		)

		if err == nil {
			t.Fatal(
				"Expected Send() to return an error for 403, got nil.",
			)
		}

		// The error should mention 403.
		if !strings.Contains(err.Error(), "403") {
			t.Errorf(
				"Expected error to mention 403. Actual error: %v",
				err,
			)
		}
	})

	// Subtest for when the agent encounters a 409 conflict, indicating it belongs to another pool.
	t.Run("409 response (node belongs to another pool)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
		}))
		defer srv.Close()

		err := Send(context.Background(), srv.Client(), srv.URL, store.Node{ID: "node-1"}, "test-token")
		if err == nil {
			t.Fatal("expected Send() to return an error for 409, got nil")
		}
		if !strings.Contains(err.Error(), "another pool") || !strings.Contains(err.Error(), "leave that pool") {
			t.Errorf("409 error should explain how to proceed, got %q", err)
		}
	})
}
