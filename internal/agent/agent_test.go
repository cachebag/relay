package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	if n.CPUs <= 0 {
		t.Errorf("Expected CPUs: > 0. Actual: %d", n.CPUs)
	}

	if n.MemBytes <= 0 {
		t.Errorf("Expected MemBytes: > 0. Actual: %d", n.MemBytes)
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

}
