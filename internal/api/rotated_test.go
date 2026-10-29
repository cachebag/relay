package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

func heartbeatWith(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"id":"node-1","hostname":"h","os":"linux","arch":"amd64","cpus":4,"mem_bytes":8000000000,"pool_token":"` + token + `"}`
	req := httptest.NewRequest(http.MethodPost, "/nodes/heartbeat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHeartbeatRotatedVsInvalidToken(t *testing.T) {
	ctx := context.Background()
	pools := store.NewMemPools()
	if err := pools.Create(ctx, store.Pool{ID: "p1", Name: "lab", Token: "old-token", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := pools.SetToken(ctx, "p1", "new-token"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	rec := heartbeatWith(t, h, "old-token")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), store.RotatedTokenMsg) {
		t.Fatalf("old token: got %d %q, want 403 with rotated message", rec.Code, rec.Body.String())
	}

	rec = heartbeatWith(t, h, "made-up-token")
	if rec.Code != http.StatusForbidden || strings.TrimSpace(rec.Body.String()) != "invalid pool token" {
		t.Fatalf("made-up token: got %d %q, want 403 invalid pool token", rec.Code, rec.Body.String())
	}
}
