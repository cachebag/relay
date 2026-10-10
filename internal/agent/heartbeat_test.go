package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// TestSendBadToken verifies that a 403 Forbidden response
// from the scheduler is returned as ErrBadToken.
func TestSendBadToken(t *testing.T) {
	// Creates a temporary HTTP server that always returns
	// 403 Forbidden to simulate an invalid pool token.
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		},
	))
	defer server.Close()

	// Sends a heartbeat to the test scheduler using a bad token.
	err := Send(
		context.Background(),
		server.Client(),
		server.URL,
		store.Node{},
		"bad-token",
	)

	// A 403 response should always produce an error.
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	// The returned error should wrap ErrBadToken so callers
	// can detect the rejected token with errors.Is.
	if !errors.Is(err, ErrBadToken) {
		t.Fatalf("expected ErrBadToken, got %v", err)
	}
}

// TestSendConnectionFailureIsNotBadToken verifies that a networking
// failure is not incorrectly reported as an invalid pool token.
func TestSendConnectionFailureIsNotBadToken(t *testing.T) {
	// Creates a temporary server so that we can obtain a valid
	// local URL for the connection failure test.
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	))

	// Saves the server URL before shutting the server down.
	// Attempting to connect to this URL afterward should fail.
	serverURL := server.URL
	server.Close()

	// Uses a short timeout so the test does not wait too long
	// if the connection cannot be established.
	client := &http.Client{
		Timeout: time.Second,
	}

	// Attempts to send a heartbeat to the closed server.
	err := Send(
		context.Background(),
		client,
		serverURL,
		store.Node{},
		"test-token",
	)

	// Since the server is closed, Send should return a
	// connection error.
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}

	// A networking failure should not be classified as
	// ErrBadToken.
	if errors.Is(err, ErrBadToken) {
		t.Fatalf(
			"expected connection error, got ErrBadToken: %v",
			err,
		)
	}
}
