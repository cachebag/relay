package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthz checks that the /healthz endpoint is working correctly.
// It verifies that the endpoint returns an HTTP 200 OK status and that the response body contains the text "ok".
func TestHealthz(t *testing.T) {

	// Create a fake HTTP response recorder.
	// This records what our server sends back without needing
	// to actually start a server on port 8080.
	rec := httptest.NewRecorder()

	// Create a fake HTTP GET request to the /healthz endpoint.
	// The final nil means this request does not contain a request body.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	// Create our API handler and send the fake request to it.
	// The response produced by the handler is stored in rec.
	NewHandler().ServeHTTP(rec, req)

	// Check that the server returned HTTP status 200 OK.
	// If it returned a different status, the test fails.
	if rec.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, rec.Code)
	}

	// Check that the response body contains exactly "ok".
	// If the body contains something different, the test fails.
	if rec.Body.String() != "ok" {
		t.Errorf("expected body %q, got %q", "ok", rec.Body.String())
	}
}
