package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Verifies the Collect() function is storing information that is
// somewhat plausible.
// Asserts: Hostname != "", CPUs > 0, MemBytes > 0.
func TestCollect(t *testing.T) {

	// Testing Collect() function.
	n, err := Collect()
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}

	// Asserting plausibility.
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

					// Responds with 204.
					w.WriteHeader(http.StatusNoContent)
				}))

		// Stops server once current function exits,
		// preventing unnecessary resource consumption.
		defer srv.Close()

		node := store.Node{
			Hostname: "test",
		}

		token := "test-token"

		// Testing Send() function.
		err := Send(
			context.Background(),
			srv.Client(),
			srv.URL,
			node,
			token,
		)
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

	// Subtest for when the scheduler returns an unsuccessful response.
	t.Run("500 response (unsuccessful)", func(t *testing.T) {

		// Creates a temporary server that responds with
		// 500 Internal Server Error.
		srv := httptest.NewServer(
			http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))

		// Stops server once current function exits,
		// preventing unnecessary resource consumption.
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
				"Expected Send() to return an error due to the scheduler " +
					"being unresponsive, got nil instead.",
			)
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

// TestTickBadTokenMessage verifies that Tick displays a helpful,
// user-facing message when the scheduler rejects the pool token.
func TestTickBadTokenMessage(t *testing.T) {

	// Creates a scheduler that rejects every heartbeat with 403.
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/nodes/heartbeat" {
					w.WriteHeader(http.StatusForbidden)
					return
				}

				// Other agent requests receive no content.
				w.WriteHeader(http.StatusNoContent)
			},
		),
	)
	defer srv.Close()

	// Creates an agent using the temporary scheduler.
	a := newAgent(
		srv.URL,
		"bad-token",
		store.Node{
			ID:       "test-node",
			Hostname: "test",
		},
		t.TempDir(),
	)

	// Uses the test server's HTTP client.
	a.client = srv.Client()

	// Captures log output so the test can verify the message
	// that would normally be displayed in the terminal.
	var logs bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()

	log.SetOutput(&logs)
	log.SetFlags(0)

	// Restores the normal logger after the test completes.
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	// Runs one interval of agent work.
	a.Tick(context.Background())

	// A rejected token should produce the friendly I62 message.
	if !strings.Contains(
		logs.String(),
		"pool token rejected; check it with your pool admin",
	) {
		t.Fatalf(
			"Expected bad token message. Actual log output:\n%s",
			logs.String(),
		)
	}
}

// TestTickConnectionFailureMessage verifies that a networking failure
// produces a helpful message containing the scheduler address and retry time.
func TestTickConnectionFailureMessage(t *testing.T) {

	// Creates a temporary server only so we can obtain a valid local URL.
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		),
	)

	// Saves the URL and then closes the server so future connections fail.
	serverURL := srv.URL
	srv.Close()

	a := newAgent(
		serverURL,
		"test-token",
		store.Node{
			ID:       "test-node",
			Hostname: "test",
		},
		t.TempDir(),
	)

	// Uses a short client timeout so the test fails quickly if the
	// unreachable server does not immediately reject the connection.
	a.client = &http.Client{
		Timeout: time.Second,
	}

	// Tick normally defaults to a 10-second retry message when it
	// is called directly instead of through Run().
	a.retryInterval = 10 * time.Second

	// Captures the log output.
	var logs bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()

	log.SetOutput(&logs)
	log.SetFlags(0)

	// Restores the normal logger after the test completes.
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	// Attempts one Tick against the unavailable scheduler.
	a.Tick(context.Background())

	output := logs.String()

	// The message should explain that the scheduler cannot be reached.
	if !strings.Contains(
		output,
		"can't reach scheduler at "+serverURL,
	) {
		t.Fatalf(
			"Expected scheduler connection failure message. Actual log output:\n%s",
			output,
		)
	}

	// The message should also tell the user when another attempt occurs.
	if !strings.Contains(output, "retrying in 10s") {
		t.Fatalf(
			"Expected retry interval in connection failure message. Actual log output:\n%s",
			output,
		)
	}
}

// TestTickConnectedMessageOnlyOnce verifies that repeated successful
// heartbeats do not repeatedly print the connected message.
func TestTickConnectedMessageOnlyOnce(t *testing.T) {

	// Creates a scheduler that accepts all requests. Returning 204 for the
	// job request represents there being no job available.
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
		),
	)
	defer srv.Close()

	a := newAgent(
		srv.URL,
		"test-token",
		store.Node{
			ID:       "test-node",
			Hostname: "test",
		},
		t.TempDir(),
	)

	a.client = srv.Client()

	// Captures log output from several successful ticks.
	var logs bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()

	log.SetOutput(&logs)
	log.SetFlags(0)

	// Restores the normal logger after the test completes.
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	ctx := context.Background()

	// Multiple successful heartbeats should only announce
	// the initial connection once.
	a.Tick(ctx)
	a.Tick(ctx)
	a.Tick(ctx)

	const connectedMessage = "connected; this machine is online in the pool"

	count := strings.Count(logs.String(), connectedMessage)

	if count != 1 {
		t.Fatalf(
			"Expected connected message once, got %d.\nLog output:\n%s",
			count,
			logs.String(),
		)
	}
}

// TestTickConnectedMessageAfterFailure verifies that the connected message
// appears again when the agent recovers after a heartbeat failure.
func TestTickConnectedMessageAfterFailure(t *testing.T) {
	heartbeatCount := 0

	// Creates a scheduler that succeeds on the first heartbeat,
	// fails on the second, and succeeds again on the third.
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {

				if r.URL.Path == "/nodes/heartbeat" {
					heartbeatCount++

					switch heartbeatCount {
					case 2:
						w.WriteHeader(http.StatusInternalServerError)
						return

					default:
						w.WriteHeader(http.StatusNoContent)
						return
					}
				}

				// No job is available.
				w.WriteHeader(http.StatusNoContent)
			},
		),
	)
	defer srv.Close()

	a := newAgent(
		srv.URL,
		"test-token",
		store.Node{
			ID:       "test-node",
			Hostname: "test",
		},
		t.TempDir(),
	)

	a.client = srv.Client()

	// Captures log output.
	var logs bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()

	log.SetOutput(&logs)
	log.SetFlags(0)

	// Restores the normal logger after the test completes.
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	ctx := context.Background()

	// First heartbeat connects.
	a.Tick(ctx)

	// Second heartbeat fails and marks the agent disconnected.
	a.Tick(ctx)

	// Third heartbeat succeeds and should announce reconnection.
	a.Tick(ctx)

	const connectedMessage = "connected; this machine is online in the pool"

	count := strings.Count(logs.String(), connectedMessage)

	if count != 2 {
		t.Fatalf(
			"Expected connected message twice after reconnection, got %d.\nLog output:\n%s",
			count,
			logs.String(),
		)
	}
}
