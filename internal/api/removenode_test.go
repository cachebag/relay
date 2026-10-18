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

// newRemoveHandler sets up two pools with one node each and the given jobs.
// Pool p1's join token is "tok1", its admin token "admin1" and its node n1;
// pool p2's are "tok2", "admin2" and n2.
func newRemoveHandler(t *testing.T, js ...store.Job) (http.Handler, *store.Mem, store.JobStore) {
	t.Helper()
	ctx := context.Background()

	pools := store.NewMemPools()
	for _, p := range []struct {
		pool  store.Pool
		admin string
	}{
		{store.Pool{ID: "p1", Name: "one", Token: "tok1"}, "admin1"},
		{store.Pool{ID: "p2", Name: "two", Token: "tok2"}, "admin2"},
	} {
		if err := pools.Create(ctx, p.pool); err != nil {
			t.Fatalf("failed to create pool: %v", err)
		}
		if err := pools.SetAdminTokenHash(ctx, p.pool.ID, store.HashToken(p.admin)); err != nil {
			t.Fatalf("failed to set admin token: %v", err)
		}
	}

	reg := store.NewMem()
	for _, n := range []store.Node{{ID: "n1", Hostname: "n1", PoolID: "p1"}, {ID: "n2", Hostname: "n2", PoolID: "p2"}} {
		n.LastSeen = time.Now().UTC()
		if err := reg.Upsert(ctx, n); err != nil {
			t.Fatalf("failed to upsert node: %v", err)
		}
	}

	jobs := store.NewMemJobs()
	for _, j := range js {
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatalf("failed to create job: %v", err)
		}
	}

	return NewHandler(reg, pools, jobs, store.NewMemCredits()), reg, jobs
}

// removeNode sends DELETE /pools/{pool}/nodes/{id} with the given admin
// token ("" for none) and returns the status code.
func removeNode(handler http.Handler, pool, id, adminToken string) int {
	req := httptest.NewRequest(http.MethodDelete, "/pools/"+pool+"/nodes/"+id, nil)
	if adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+adminToken)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

// heartbeatAs sends a heartbeat for node id with the given join token.
func heartbeatAs(handler http.Handler, id, poolToken string) *httptest.ResponseRecorder {
	body := `{"id":"` + id + `","hostname":"` + id + `","pool_token":"` + poolToken + `"}`
	req := httptest.NewRequest(http.MethodPost, "/nodes/heartbeat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// listedNodeIDs returns the IDs GET /nodes reports.
func listedNodeIDs(t *testing.T, handler http.Handler) map[string]bool {
	t.Helper()
	rec := getJSON(handler, "/nodes")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /nodes: expected 200, got %d", rec.Code)
	}
	var nodes []store.Node
	if err := json.NewDecoder(rec.Body).Decode(&nodes); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range nodes {
		ids[n.ID] = true
	}
	return ids
}

// TestAdminRemoveNode checks that removing a machine returns 204, drops it
// from GET /nodes, puts its running job back in the queue with the attempt
// given back, and that the machine's next heartbeat gets 410.
func TestAdminRemoveNode(t *testing.T) {
	handler, _, jobs := newRemoveHandler(t,
		store.Job{ID: "running", Command: []string{"a"}, Status: store.JobRunning, NodeID: "n1", PoolID: "p1", Attempts: 1},
		store.Job{ID: "other", Command: []string{"b"}, Status: store.JobRunning, NodeID: "n2", PoolID: "p2", Attempts: 1},
	)

	if code := removeNode(handler, "p1", "n1", "admin1"); code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", code)
	}

	if ids := listedNodeIDs(t, handler); ids["n1"] || !ids["n2"] {
		t.Fatalf("expected only n2 in GET /nodes, got %v", ids)
	}

	j, err := jobs.Get(context.Background(), "running")
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if j.Status != store.JobQueued || j.NodeID != "" || j.Attempts != 0 {
		t.Errorf("expected job queued with no node and attempts 0, got status=%s node=%q attempts=%d",
			j.Status, j.NodeID, j.Attempts)
	}

	other, _ := jobs.Get(context.Background(), "other")
	if other.Status != store.JobRunning || other.NodeID != "n2" || other.Attempts != 1 {
		t.Errorf("expected job on n2 unchanged, got %+v", other)
	}

	// The removed machine's heartbeat is refused and doesn't bring it back.
	rec := heartbeatAs(handler, "n1", "tok1")
	if rec.Code != http.StatusGone {
		t.Fatalf("expected heartbeat to get 410, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "removed from pool by admin") {
		t.Errorf("expected 410 body to say why, got %q", rec.Body.String())
	}
	if ids := listedNodeIDs(t, handler); ids["n1"] {
		t.Error("removed node came back after heartbeating")
	}

	// Other machines keep heartbeating normally.
	if rec := heartbeatAs(handler, "n2", "tok2"); rec.Code != http.StatusNoContent {
		t.Errorf("expected n2's heartbeat to get 204, got %d", rec.Code)
	}
}

// TestAdminRemoveNodeRejected checks that a wrong or missing admin token
// gets 401, that a machine from another pool or an unknown machine gets 404,
// and that none of them change anything.
func TestAdminRemoveNodeRejected(t *testing.T) {
	running := store.Job{ID: "running", Command: []string{"a"}, Status: store.JobRunning, NodeID: "n1", PoolID: "p1", Attempts: 1}

	cases := []struct {
		name  string
		pool  string
		node  string
		token string
		want  int
	}{
		{"no admin token", "p1", "n1", "", http.StatusUnauthorized},
		{"wrong admin token", "p1", "n1", "nope", http.StatusUnauthorized},
		{"join token instead of admin token", "p1", "n1", "tok1", http.StatusUnauthorized},
		{"other pool's admin token", "p1", "n1", "admin2", http.StatusUnauthorized},
		{"unknown pool", "ghost", "n1", "admin1", http.StatusUnauthorized},
		{"machine from another pool", "p1", "n2", "admin1", http.StatusNotFound},
		{"unknown machine", "p1", "ghost", "admin1", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler, reg, jobs := newRemoveHandler(t, running)

			if code := removeNode(handler, c.pool, c.node, c.token); code != c.want {
				t.Fatalf("expected %d, got %d", c.want, code)
			}

			if ids := nodeIDs(t, reg); !ids["n1"] || !ids["n2"] {
				t.Errorf("expected both nodes to stay, got %v", ids)
			}
			j, _ := jobs.Get(context.Background(), "running")
			if j.Status != store.JobRunning || j.NodeID != "n1" || j.Attempts != 1 {
				t.Errorf("expected job unchanged, got %+v", j)
			}
			for _, n := range []struct{ id, pool string }{{"n1", "p1"}, {"n2", "p2"}} {
				if removed, _ := reg.HasTombstone(context.Background(), n.pool, n.id); removed {
					t.Errorf("expected no tombstone for %s", n.id)
				}
			}
		})
	}
}