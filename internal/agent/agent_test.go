package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		var receivedBody []byte
		var receivedNode store.Node

		// Creates temporary server with a handler that captures
		// the request body and responds with 204 (success).
		srv := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {

					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Errorf("Failed to read request body: %v", err)
						return
					}

					receivedBody = body

					// Responds with 204
					w.WriteHeader(http.StatusNoContent)
				}))

		// Stops server once current function exit,
		// preventing unneccesary resource consumption.
		defer srv.Close()

		node := store.Node{
			Hostname: "test",
		}

		// Testing Send() function
		err := Send(context.Background(), srv.Client(), srv.URL, node)
		if err != nil {
			t.Fatalf("Send() error: %v", err)
		}

		// Testing whether received body causes errors being unmarshalled.
		err = json.Unmarshal(receivedBody, &receivedNode)
		if err != nil {
			t.Fatalf("Failed to unmarshal received body: %v", err)
		}

		// Testing whether heartbeat contains correct Hostname
		if receivedNode.Hostname != node.Hostname {
			t.Errorf(
				"Expected Hostname: %q. Actual Hostname: %q",
				node.Hostname, receivedNode.Hostname)

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

		err := Send(
			context.Background(),
			srv.Client(),
			srv.URL,
			node,
		)
		if err == nil {
			t.Fatal(
				"Expected Send() to return an error due to the scheduler",
				"being unresponsive, got nil instead.")
		}

	})
}
