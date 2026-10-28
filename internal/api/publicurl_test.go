package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

const testPublicURL = "https://relay.tail1234.ts.net"

func newPublicHandler(url *string) http.Handler {
	return NewHandler(store.NewMem(), store.NewMemPools(), store.NewMemJobs(),
		store.NewMemCredits(), testServer(),
		WithPublicURL(func() string { return *url }))
}

func createPoolJSON(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()
	rec := send(handler, http.MethodPost, "/pools", testServerToken, `{"name":"lab"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pool: expected 201, got %d: %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return body
}

func rotateJSON(t *testing.T, handler http.Handler, poolID, adminToken string) map[string]any {
	t.Helper()
	rec := send(handler, http.MethodPost, "/pools/"+poolID+"/token", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate token: expected 200, got %d: %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	return body
}

func TestPoolCreatePublicURL(t *testing.T) {
	url := testPublicURL
	body := createPoolJSON(t, newPublicHandler(&url))

	if got := body["public_url"]; got != testPublicURL {
		t.Errorf("expected public_url %q, got %v", testPublicURL, got)
	}
	// The tokens are still there.
	if body["token"] == "" || body["admin_token"] == "" {
		t.Errorf("expected token and admin_token in the response, got %v", body)
	}
}

func TestRotateTokenPublicURL(t *testing.T) {
	url := testPublicURL
	handler := newPublicHandler(&url)
	created := createPoolJSON(t, handler)

	body := rotateJSON(t, handler, created["id"].(string), created["admin_token"].(string))
	if got := body["public_url"]; got != testPublicURL {
		t.Errorf("expected public_url %q, got %v", testPublicURL, got)
	}
	if body["token"] == "" || body["token"] == created["token"] {
		t.Errorf("expected a new token, got %v", body["token"])
	}
}

func TestPublicURLEmptyWhenNotPublic(t *testing.T) {
	handler := NewHandler(store.NewMem(), store.NewMemPools(), store.NewMemJobs(),
		store.NewMemCredits(), testServer())

	created := createPoolJSON(t, handler)
	if got, ok := created["public_url"]; !ok || got != "" {
		t.Errorf("create: expected public_url \"\", got %v (present: %v)", got, ok)
	}

	rotated := rotateJSON(t, handler, created["id"].(string), created["admin_token"].(string))
	if got, ok := rotated["public_url"]; !ok || got != "" {
		t.Errorf("rotate: expected public_url \"\", got %v (present: %v)", got, ok)
	}
}

func TestPublicURLReadPerRequest(t *testing.T) {
	url := ""
	handler := newPublicHandler(&url)

	if got := createPoolJSON(t, handler)["public_url"]; got != "" {
		t.Errorf("before the address is known: expected \"\", got %v", got)
	}

	url = testPublicURL
	if got := createPoolJSON(t, handler)["public_url"]; got != testPublicURL {
		t.Errorf("after the address is known: expected %q, got %v", testPublicURL, got)
	}
}
