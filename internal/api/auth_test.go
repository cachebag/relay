package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// newTwoPoolHandler sets up pools p1 (token tok1, node n1) and p2 (token
// tok2, node n2), each with one running job on its node: job1 and job2.
func newTwoPoolHandler(t *testing.T) (http.Handler, store.JobStore) {
	t.Helper()
	ctx := context.Background()

	pools := store.NewMemPools()
	reg := store.NewMem()
	jobs := store.NewMemJobs()
	for _, n := range []string{"1", "2"} {
		p := store.Pool{ID: "p" + n, Name: "pool " + n, Token: "tok" + n, CreatedAt: time.Now().UTC()}
		if err := pools.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		node := store.Node{ID: "n" + n, Hostname: "n" + n, PoolID: p.ID, LastSeen: time.Now().UTC()}
		if err := reg.Upsert(ctx, node); err != nil {
			t.Fatal(err)
		}
		j := store.Job{ID: "job" + n, Command: []string{"x"}, Status: store.JobRunning, NodeID: node.ID,
			PoolID: p.ID, Submitter: "someone", Attempts: 1, CreatedAt: time.Now().UTC()}
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(reg, pools, jobs, store.NewMemCredits(), testServer()), jobs
}

// send makes a request with the given bearer token ("" for none).
func send(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestNextJobNeedsPoolToken checks #94: jobs/next with no token, a wrong
// token or another pool's token is 403, and the right token still works.
func TestNextJobNeedsPoolToken(t *testing.T) {
	handler, _ := newTwoPoolHandler(t)

	for _, tc := range []struct{ name, token string }{
		{"no token", ""}, {"wrong token", "nope"}, {"other pool's token", "tok2"},
	} {
		if rec := send(handler, http.MethodGet, "/nodes/n1/jobs/next", tc.token, ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", tc.name, rec.Code)
		}
	}

	rec := send(handler, http.MethodGet, "/nodes/n1/jobs/next", "tok1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("right token: expected 200, got %d", rec.Code)
	}
	var got store.Job
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "job1" {
		t.Errorf("expected job1, got %q", got.ID)
	}
}

// TestResultNeedsPoolToken checks #94: a result with no token, a wrong
// token or another pool's token is 403 and changes nothing; the right
// token is accepted as before.
func TestResultNeedsPoolToken(t *testing.T) {
	handler, jobs := newTwoPoolHandler(t)
	body := `{"node_id":"n1","attempt":1,"exit_code":0}`

	for _, tc := range []struct{ name, token string }{
		{"no token", ""}, {"wrong token", "nope"}, {"other pool's token", "tok2"},
	} {
		if rec := send(handler, http.MethodPost, "/jobs/job1/result", tc.token, body); rec.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", tc.name, rec.Code)
		}
		j, _ := jobs.Get(context.Background(), "job1")
		if j.Status != store.JobRunning {
			t.Errorf("%s: job changed to %q", tc.name, j.Status)
		}
	}

	if rec := send(handler, http.MethodPost, "/jobs/job1/result", "tok1", body); rec.Code != http.StatusNoContent {
		t.Fatalf("right token: expected 204, got %d", rec.Code)
	}
	j, _ := jobs.Get(context.Background(), "job1")
	if j.Status != store.JobDone {
		t.Errorf("expected done, got %q", j.Status)
	}
}

// TestReadRoutesNeedPoolToken checks #95: GET /nodes, /jobs, /jobs/{id} and
// /jobs/{id}/output are 403 with no token or a wrong one.
func TestReadRoutesNeedPoolToken(t *testing.T) {
	handler, _ := newTwoPoolHandler(t)
	for _, path := range []string{"/nodes", "/jobs", "/jobs/job1", "/jobs/job1/output"} {
		for _, token := range []string{"", "nope"} {
			if rec := send(handler, http.MethodGet, path, token, ""); rec.Code != http.StatusForbidden {
				t.Errorf("%s with token %q: expected 403, got %d", path, token, rec.Code)
			}
		}
	}
}

// TestReadRoutesOnlyShowOwnPool checks #95: each token sees only its own
// pool's nodes and jobs, and the other pool's job is a 404 by ID.
func TestReadRoutesOnlyShowOwnPool(t *testing.T) {
	handler, _ := newTwoPoolHandler(t)

	// Finish job1 so its output can be read.
	if rec := send(handler, http.MethodPost, "/jobs/job1/result", "tok1", `{"node_id":"n1","attempt":1,"exit_code":0,"stdout":"hi"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("result: expected 204, got %d", rec.Code)
	}

	ids := func(rec *httptest.ResponseRecorder) []string {
		t.Helper()
		var rows []struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}

	rec := send(handler, http.MethodGet, "/nodes", "tok1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes: expected 200, got %d", rec.Code)
	}
	if got := ids(rec); len(got) != 1 || got[0] != "n1" {
		t.Errorf("tok1 nodes: expected [n1], got %v", got)
	}

	rec = send(handler, http.MethodGet, "/jobs", "tok2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("jobs: expected 200, got %d", rec.Code)
	}
	if got := ids(rec); len(got) != 1 || got[0] != "job2" {
		t.Errorf("tok2 jobs: expected [job2], got %v", got)
	}

	// Own job by ID works; the other pool's job looks like it doesn't exist.
	if rec := send(handler, http.MethodGet, "/jobs/job1", "tok1", ""); rec.Code != http.StatusOK {
		t.Errorf("own job: expected 200, got %d", rec.Code)
	}
	if rec := send(handler, http.MethodGet, "/jobs/job1/output", "tok1", ""); rec.Code != http.StatusOK {
		t.Errorf("own output: expected 200, got %d", rec.Code)
	}
	for _, path := range []string{"/jobs/job1", "/jobs/job1/output"} {
		if rec := send(handler, http.MethodGet, path, "tok2", ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s with other pool's token: expected 404, got %d", path, rec.Code)
		}
	}
}

// TestPoolRoutesNeedServerAdminToken checks #96: POST /pools and GET /pools
// are 401 with no token, a wrong token or a pool's join token, and work with
// the server admin token.
func TestPoolRoutesNeedServerAdminToken(t *testing.T) {
	handler, _ := newTwoPoolHandler(t)

	for _, tc := range []struct{ name, token string }{
		{"no token", ""}, {"wrong token", "nope"}, {"join token", "tok1"},
	} {
		if rec := send(handler, http.MethodPost, "/pools", tc.token, `{"name":"x"}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("POST /pools %s: expected 401, got %d", tc.name, rec.Code)
		}
		if rec := send(handler, http.MethodGet, "/pools", tc.token, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET /pools %s: expected 401, got %d", tc.name, rec.Code)
		}
	}

	if rec := send(handler, http.MethodPost, "/pools", testServerToken, `{"name":"x"}`); rec.Code != http.StatusCreated {
		t.Errorf("POST /pools with server admin token: expected 201, got %d", rec.Code)
	}
	rec := send(handler, http.MethodGet, "/pools", testServerToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /pools with server admin token: expected 200, got %d", rec.Code)
	}
	var listed []store.Pool
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 {
		t.Errorf("expected 3 pools, got %d", len(listed))
	}
}

// TestPoolRoutesWithNoServerTokenYet checks that a relayd whose server
// store has no token refuses everything, rather than letting anyone in.
func TestPoolRoutesWithNoServerTokenYet(t *testing.T) {
	handler := NewHandler(store.NewMem(), store.NewMemPools(), store.NewMemJobs(), store.NewMemCredits(), store.NewMemServer())
	if rec := send(handler, http.MethodPost, "/pools", "anything", `{"name":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
