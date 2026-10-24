package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// TestHealthz checks that the /healthz endpoint is working correctly.
// It verifies that the endpoint returns an HTTP 200 OK status and that the response body contains the text "ok".
func TestHealthz(t *testing.T) {

	// Create an in-memory registry for the API handler.
	reg := store.NewMem()

	// Creates an in-memory store for pools.
	pools := store.NewMemPools()

	// Create a fake HTTP response recorder.
	// This records what our server sends back without needing
	// to actually start a server on port 8080.
	rec := httptest.NewRecorder()

	// Create a fake HTTP GET request to the /healthz endpoint.
	// The final nil means this request does not contain a request body.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	// Create our API handler and send the fake request to it.
	// The response produced by the handler is stored in rec.
	NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer()).ServeHTTP(rec, req)

	// Check that the server returned HTTP status 200 OK.
	if rec.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, rec.Code)
	}

	// Check that the response body contains exactly "ok".
	if rec.Body.String() != "ok" {
		t.Errorf("expected body %q, got %q", "ok", rec.Body.String())
	}
}

// TestNodeHeartbeatAndList checks that a node can send a heartbeat
// and then appear in the list of registered nodes.
func TestNodeHeartbeatAndList(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	// Create a pool first so we can get a valid pool token.
	poolBody := `{
		"name":"test-pool"
	}`

	poolReq := httptest.NewRequest(
		http.MethodPost,
		"/pools",
		strings.NewReader(poolBody),
	)
	poolReq.Header.Set("Content-Type", "application/json")
	poolReq.Header.Set("Authorization", "Bearer "+testServerToken)

	poolRec := httptest.NewRecorder()

	handler.ServeHTTP(poolRec, poolReq)

	// Pool creation should return 201 Created.
	if poolRec.Code != http.StatusCreated {
		t.Fatalf("expected pool status 201, got %d", poolRec.Code)
	}

	// Decode the created pool so we can access its ID and token.
	var pool store.Pool

	if err := json.Unmarshal(poolRec.Body.Bytes(), &pool); err != nil {
		t.Fatalf("failed to decode pool response: %v", err)
	}

	// JSON representing a node sending a heartbeat.
	// The node must include the valid pool token.
	body := fmt.Sprintf(`{
		"id":"node-1",
		"hostname":"test-host",
		"os":"linux",
		"arch":"amd64",
		"cpus":4,
		"mem_bytes":8000000000,
		"pool_token":%q
	}`, pool.Token)

	// Send a fake POST request to the heartbeat endpoint.
	req := httptest.NewRequest(
		http.MethodPost,
		"/nodes/heartbeat",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// A successful heartbeat should return 204 No Content.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}

	// Request the nodes with this pool's token.
	req = httptest.NewRequest(
		http.MethodGet,
		"/nodes",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+pool.Token)

	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// GET /nodes should return 200 OK.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Convert the JSON response back into Node objects.
	var nodes []store.Node

	if err := json.Unmarshal(rec.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Make sure exactly one node was stored.
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	// Make sure the hostname was stored correctly.
	if nodes[0].Hostname != "test-host" {
		t.Errorf("expected hostname test-host, got %q", nodes[0].Hostname)
	}

	// Make sure the server assigned the node to the correct pool.
	if nodes[0].PoolID != pool.ID {
		t.Errorf("expected pool ID %q, got %q", pool.ID, nodes[0].PoolID)
	}

	// Make sure the server assigned a LastSeen time.
	if nodes[0].LastSeen.IsZero() {
		t.Error("expected LastSeen to be set")
	}

	// Request nodes with another pool's token.
	other := store.Pool{ID: "other", Name: "other", Token: "other-token", CreatedAt: time.Now().UTC()}
	if err := pools.Create(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(
		http.MethodGet,
		"/nodes",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+other.Token)

	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// The request should still succeed.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// No nodes belong to this pool, so the response should be an empty array.
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected body [], got %s", rec.Body.String())
	}

}

// TestNodeHeartbeatInvalidJSON checks that malformed JSON
// is rejected with HTTP 400 Bad Request.
func TestNodeHeartbeatInvalidJSON(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	req := httptest.NewRequest(
		http.MethodPost,
		"/nodes/heartbeat",
		strings.NewReader("not json"),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

// TestNodeListEmpty checks that a fresh registry returns an empty
// JSON array instead of null when there are no registered nodes.
func TestNodeListEmpty(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	req := httptest.NewRequest(
		http.MethodGet,
		"/nodes",
		nil,
	)
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected body [], got %s", rec.Body.String())
	}
}

// TestPoolCreateAndList checks that a pool can be created
// and that its secret token is hidden when pools are listed.
func TestPoolCreateAndList(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	// Create a new pool.
	body := `{
		"name":"test-pool"
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/pools",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testServerToken)

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Creating a pool should return 201 Created.
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	// Decode the created pool.
	var created store.Pool

	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode pool response: %v", err)
	}

	// Make sure an ID was generated.
	if created.ID == "" {
		t.Error("expected pool ID to be generated")
	}

	// Make sure a token was generated.
	if created.Token == "" {
		t.Error("expected pool token to be generated")
	}

	// The ticket requires a 32-character token.
	if len(created.Token) != 32 {
		t.Errorf("expected token length 32, got %d", len(created.Token))
	}

	// Now list all pools.
	req = httptest.NewRequest(
		http.MethodGet,
		"/pools",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+testServerToken)

	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var listed []store.Pool

	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("failed to decode pool list: %v", err)
	}

	// We only created one pool.
	if len(listed) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(listed))
	}

	// Make sure the correct pool was returned.
	if listed[0].ID != created.ID {
		t.Errorf("expected pool ID %q, got %q", created.ID, listed[0].ID)
	}

	// The secret token must not be exposed by GET /pools.
	if listed[0].Token != "" {
		t.Errorf("expected pool token to be hidden, got %q", listed[0].Token)
	}
}

// TestPoolSelf checks that GET /pools/self returns the pool owning the bearer
// token without exposing the token, and rejects bad or missing credentials.
func TestPoolSelf(t *testing.T) {
	pools := store.NewMemPools()
	handler := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	p := store.Pool{ID: "pool-1", Name: "lab", Token: "secret-token", CreatedAt: time.Now().UTC()}
	if err := pools.Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}

	get := func(auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/pools/self", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	rec := get("Bearer secret-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "pool-1" || got["name"] != "lab" {
		t.Errorf("unexpected body: %v", got)
	}
	if _, ok := got["token"]; ok {
		t.Error("token must not be returned")
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Error("response leaks the token")
	}

	if rec := get("Bearer wrong"); rec.Code != http.StatusForbidden {
		t.Errorf("wrong token: expected 403, got %d", rec.Code)
	}
	if rec := get(""); rec.Code != http.StatusForbidden {
		t.Errorf("no header: expected 403, got %d", rec.Code)
	}
}

// TestNodeHeartbeatInvalidPoolToken checks that a heartbeat
// with an invalid pool token is rejected with 403 Forbidden.
func TestNodeHeartbeatInvalidPoolToken(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	body := `{
		"id":"node-1",
		"hostname":"test-host",
		"os":"linux",
		"arch":"amd64",
		"cpus":4,
		"mem_bytes":8000000000,
		"pool_token":"invalid-token"
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/nodes/heartbeat",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
}

// Checks that a heartbeat from a node with an ID that belongs
// to a different pool is rejected with 409 Conflict.
func TestNodeHeartbeatRejectsIDFromAnotherPool(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	poolA := store.Pool{ID: "pool-a", Name: "Pool A", Token: "token-a"}
	poolB := store.Pool{ID: "pool-b", Name: "Pool B", Token: "token-b"}
	if err := pools.Create(context.Background(), poolA); err != nil {
		t.Fatal(err)
	}
	if err := pools.Create(context.Background(), poolB); err != nil {
		t.Fatal(err)
	}

	original := store.Node{
		ID:       "shared-node-id",
		PoolID:   poolA.ID,
		Hostname: "first-machine",
		OS:       "linux",
		Arch:     "amd64",
		CPUs:     4,
		MemBytes: 8_000_000_000,
		LastSeen: time.Now().Add(-time.Hour).UTC(),
	}
	if err := reg.Upsert(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	sendHeartbeat := func(token, hostname string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{
			"id":%q,"hostname":%q,"os":"linux","arch":"amd64",
			"cpus":8,"mem_bytes":16000000000,"pool_token":%q
		}`, original.ID, hostname, token)
		req := httptest.NewRequest(http.MethodPost, "/nodes/heartbeat", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Send a heartbeat with a token from a different pool and expect a 409 Conflict.
	rec := sendHeartbeat(poolB.Token, "changed-machine")
	if rec.Code != http.StatusConflict {
		t.Fatalf("cross-pool heartbeat status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "leave that pool") {
		t.Errorf("conflict response should explain how to proceed, got %q", rec.Body.String())
	}
	nodes, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0] != original {
		t.Fatalf("cross-pool heartbeat changed stored node: got %+v, want %+v", nodes, original)
	}

	// Send a heartbeat with a token from the same pool and expect a 204 No Content.
	rec = sendHeartbeat(poolA.Token, "updated-machine")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("same-pool heartbeat status = %d, want 204", rec.Code)
	}
	nodes, err = reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].PoolID != poolA.ID || nodes[0].Hostname != "updated-machine" {
		t.Fatalf("same-pool heartbeat did not update node: %+v", nodes)
	}
}

// createTestPool creates a pool with a known join token for API tests.
func createTestPool(t *testing.T, pools store.PoolStore) store.Pool {
	t.Helper()
	p := store.Pool{
		ID: "test-pool", Name: "Test Pool", Token: "test-token", CreatedAt: time.Now().UTC(),
	}
	if err := pools.Create(context.Background(), p); err != nil {
		t.Fatalf("failed to create test pool: %v", err)
	}
	return p
}

// submitTestJob sends an authenticated job submission request.
func submitTestJob(handler http.Handler, command []string, submitter string, os string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(struct {
		Command   []string `json:"command"`
		Submitter string   `json:"submitter"`
		Requires  struct {
			OS string `json:"os"`
		} `json:"requires"`
	}{Command: command, Submitter: submitter, Requires: struct {
		OS string `json:"os"`
	}{OS: os}})
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestJobCreate verifies that a new job gets the authenticated pool's ID and the submitter.
func TestJobCreate(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	pool := createTestPool(t, pools)
	handler := NewHandler(reg, pools, jobs, store.NewMemCredits(), testServer())

	rec := submitTestJob(handler, []string{"echo", "hi"}, "alice", "linux")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ID     string          `json:"id"`
		Status store.JobStatus `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(response.ID) != 12 {
		t.Errorf("expected job ID length 12, got %d", len(response.ID))
	}
	if response.Status != store.JobQueued {
		t.Errorf("expected queued status, got %q", response.Status)
	}
	created, err := jobs.Get(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("failed to retrieve created job: %v", err)
	}
	if len(created.Command) != 2 || created.Command[0] != "echo" || created.Command[1] != "hi" {
		t.Errorf("unexpected command: %v", created.Command)
	}
	if created.Status != store.JobQueued {
		t.Errorf("expected stored status queued, got %q", created.Status)
	}
	if created.PoolID != pool.ID {
		t.Errorf("expected pool ID %q, got %q", pool.ID, created.PoolID)
	}
	if created.Submitter != "alice" {
		t.Errorf("expected submitter alice, got %q", created.Submitter)
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected creation time to be set")
	}
}

// TestJobCreateEmptyCommand checks that an empty command is rejected.
func TestJobCreateEmptyCommand(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())
	rec := submitTestJob(handler, []string{}, "alice", "linux")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestJobCreateInvalidPoolToken(t *testing.T) {
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, jobs, store.NewMemCredits(), testServer())

	tests := []struct{ name, header string }{
		{"missing token", ""},
		{"invalid token", "Bearer wrong-token"},
		{"missing Bearer prefix", "test-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"command":["echo","hello"],"submitter":"alice"}`))
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("expected status 403, got %d", rec.Code)
			}
		})
	}
	listed, err := jobs.List(context.Background())
	if err != nil {
		t.Fatalf("failed to list jobs: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("expected no jobs, got %d", len(listed))
	}
}

func TestJobCreateEmptySubmitter(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())
	for _, submitter := range []string{"", "   "} {
		rec := submitTestJob(handler, []string{"echo", "hello"}, submitter, "windows")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("submitter %q: expected 400, got %d", submitter, rec.Code)
		}
	}
}

// TestJobCreateUsesAuthenticatedPool confirms a client-supplied pool_id is ignored.
func TestJobCreateUsesAuthenticatedPool(t *testing.T) {
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	pool := createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, jobs, store.NewMemCredits(), testServer())
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(`{"command":["echo","hello"],"submitter":"alice","pool_id":"another-pool"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Get(context.Background(), response.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.PoolID != pool.ID {
		t.Errorf("expected pool %q, got %q", pool.ID, job.PoolID)
	}
	if job.Submitter != "alice" {
		t.Errorf("expected alice, got %q", job.Submitter)
	}
}

// TestJobCreateRequiredOS checks that requires.os is stored and echoed.
func TestJobCreateRequiredOS(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), pools, jobs, store.NewMemCredits(), testServer())
	rec := submitTestJob(handler, []string{"echo", "hello"}, "omar", "linux")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Requires.OS != "linux" {
		t.Errorf("expected requires.os to be 'linux', got %q", job.Requires.OS)
	}
	if job.Submitter != "omar" {
		t.Errorf("expected submitter to be 'omar', got %q", job.Submitter)
	}
}

// TestJobGet checks that a submitted job can be retrieved by its ID.
func TestJobGet(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())
	rec := submitTestJob(handler, []string{"echo", "hi"}, "alice", "linux")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rec = getJSON(handler, "/jobs/"+created.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var job store.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ID != created.ID {
		t.Errorf("expected job ID %q, got %q", created.ID, job.ID)
	}
	if len(job.Command) != 2 || job.Command[0] != "echo" || job.Command[1] != "hi" {
		t.Errorf("unexpected command: %v", job.Command)
	}
	if job.Status != store.JobQueued {
		t.Errorf("expected queued, got %q", job.Status)
	}
	if job.PoolID != "test-pool" || job.Submitter != "alice" {
		t.Errorf("unexpected job ownership: %+v", job)
	}
}

// TestJobGetNotFound checks that requesting an unknown job returns 404.
func TestJobGetNotFound(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())
	rec := getJSON(handler, "/jobs/doesnotexist")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

// TestJobList checks that submitted jobs appear in the list endpoint.
func TestJobList(t *testing.T) {
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(store.NewMem(), pools, store.NewMemJobs(), store.NewMemCredits(), testServer())
	for _, tc := range []struct{ command, submitter string }{{"first", "alice"}, {"second", "bob"}} {
		rec := submitTestJob(handler, []string{"echo", tc.command}, tc.submitter, "darwin")
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
	}
	rec := getJSON(handler, "/jobs")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var listed []store.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(listed))
	}
	found := map[string]string{}
	for _, job := range listed {
		if len(job.Command) != 2 {
			t.Errorf("unexpected command: %v", job.Command)
			continue
		}
		if job.PoolID != "test-pool" {
			t.Errorf("unexpected pool %q", job.PoolID)
		}
		found[job.Command[1]] = job.Submitter
	}
	if found["first"] != "alice" || found["second"] != "bob" {
		t.Errorf("unexpected submitted jobs: %v", found)
	}
}

// TestJobListFilterByPool checks that each pool's token lists only that pool's jobs.
func TestJobListFilterByPool(t *testing.T) {
	jobs := store.NewMemJobs()
	pools := store.NewMemPools()
	for _, p := range []store.Pool{{ID: "pool-a", Name: "a", Token: "tok-a"}, {ID: "pool-b", Name: "b", Token: "tok-b"}} {
		if err := pools.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(store.NewMem(), pools, jobs, store.NewMemCredits(), testServer())
	for _, job := range []store.Job{
		{ID: "job-a1", PoolID: "pool-a", Submitter: "alice", Status: store.JobQueued},
		{ID: "job-a2", PoolID: "pool-a", Submitter: "bob", Status: store.JobQueued},
		{ID: "job-b1", PoolID: "pool-b", Submitter: "charlie", Status: store.JobQueued},
	} {
		if err := jobs.Create(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, token, pool string
		expected          int
	}{
		{"pool A", "tok-a", "pool-a", 2},
		{"pool B", "tok-b", "pool-b", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := getAs(handler, "/jobs", tc.token)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			var listed []store.Job
			if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			if len(listed) != tc.expected {
				t.Fatalf("expected %d jobs, got %d", tc.expected, len(listed))
			}
			if tc.pool != "" {
				for _, job := range listed {
					if job.PoolID != tc.pool {
						t.Errorf("job %s is in pool %q, expected %q", job.ID, job.PoolID, tc.pool)
					}
				}
			}
		})
	}
}

// TestJobListEmpty checks that a fresh job store returns
// an empty JSON array instead of null.
func TestJobListEmpty(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	createTestPool(t, pools)
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs, store.NewMemCredits(), testServer())

	rec := getJSON(handler, "/jobs")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected body [], got %s", rec.Body.String())
	}
}

// postResult sends a result for a job and returns the response status code.
func postResult(handler http.Handler, jobID, body string) int {
	req := httptest.NewRequest(
		http.MethodPost,
		"/jobs/"+jobID+"/result",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec.Code
}

// TestPostJobResultStale checks that results from the wrong node,
// an old attempt, or an already-done job are rejected with 409.
func TestPostJobResultStale(t *testing.T) {
	newHandler := func(j store.Job) (http.Handler, store.JobStore) {
		return newJobHandler(t, j)
	}

	assigned := store.Job{
		ID:       "job1",
		Command:  []string{"echo", "hi"},
		Status:   store.JobAssigned,
		NodeID:   "node1",
		Attempts: 2,
	}

	t.Run("correct node and attempt returns 204", func(t *testing.T) {
		handler, jobs := newHandler(assigned)

		code := postResult(handler, "job1", `{"node_id":"node1","attempt":2,"exit_code":0}`)
		if code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", code)
		}

		j, err := jobs.Get(context.Background(), "job1")
		if err != nil {
			t.Fatalf("failed to get job: %v", err)
		}
		if j.Status != store.JobDone {
			t.Errorf("expected job status %q, got %q", store.JobDone, j.Status)
		}
	})

	t.Run("older attempt returns 409 and job is unchanged", func(t *testing.T) {
		handler, jobs := newHandler(assigned)

		code := postResult(handler, "job1", `{"node_id":"node1","attempt":1,"exit_code":0}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", code)
		}

		j, _ := jobs.Get(context.Background(), "job1")
		if j.Status != store.JobAssigned {
			t.Errorf("expected job to stay %q, got %q", store.JobAssigned, j.Status)
		}
	})

	t.Run("wrong node_id returns 409", func(t *testing.T) {
		handler, _ := newHandler(assigned)

		code := postResult(handler, "job1", `{"node_id":"node2","attempt":2,"exit_code":0}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", code)
		}
	})

	t.Run("already done job returns 409", func(t *testing.T) {
		done := assigned
		done.Status = store.JobDone

		handler, _ := newHandler(done)

		code := postResult(handler, "job1", `{"node_id":"node1","attempt":2,"exit_code":0}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", code)
		}
	})
}

// testServerToken is the server admin token every test handler accepts.
const testServerToken = "server-admin"

// testServer returns a server store that knows testServerToken.
func testServer() store.ServerStore {
	s := store.NewMemServer()
	s.SetAdminTokenHash(context.Background(), store.HashToken(testServerToken))
	return s
}

// getJSON sends a GET with the test pool's join token and returns the recorder.
func getJSON(handler http.Handler, path string) *httptest.ResponseRecorder {
	return getAs(handler, path, "test-token")
}

// getAs sends a GET with the given bearer token ("" for none).
func getAs(handler http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// Creates a new HTTP handler and in-memory job store that is pre-populated
// with the given jobs.
// Every job is put in the test pool unless it names another, and every node
// a job mentions is registered there, so the token checks pass.
func newJobHandler(t *testing.T, js ...store.Job) (http.Handler, store.JobStore) {
	t.Helper()
	ctx := context.Background()
	pools := store.NewMemPools()
	pool := createTestPool(t, pools)
	reg := store.NewMem()
	jobs := store.NewMemJobs()
	for _, j := range js {
		if j.PoolID == "" {
			j.PoolID = pool.ID
		}
		if j.NodeID != "" {
			n := store.Node{ID: j.NodeID, Hostname: j.NodeID, PoolID: j.PoolID, LastSeen: time.Now().UTC()}
			if err := reg.Upsert(ctx, n); err != nil {
				t.Fatalf("failed to upsert node: %v", err)
			}
		}
		if err := jobs.Create(ctx, j); err != nil {
			t.Fatalf("failed to create job: %v", err)
		}
	}
	return NewHandler(reg, pools, jobs, store.NewMemCredits(), testServer()), jobs
}

// TestNextJobNothingAssigned checks that a node with no work, or an unknown
// node, gets 204.
func TestNextJobNothingAssigned(t *testing.T) {
	handler, _ := newJobHandler(t,
		store.Job{ID: "q", Status: store.JobQueued},
		store.Job{ID: "other", Status: store.JobAssigned, NodeID: "node2"},
		store.Job{ID: "fin", Status: store.JobDone, NodeID: "node1"},
	)
	if rec := getJSON(handler, "/nodes/node1/jobs/next"); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	// A node the pool has never heard of can't take jobs.
	if rec := getJSON(handler, "/nodes/ghost/jobs/next"); rec.Code != http.StatusForbidden {
		t.Fatalf("unknown node: expected 403, got %d", rec.Code)
	}
}

// TestNextJobReturnsOldestForNode checks that the oldest job for the node is
// returned, marked running, and handed back again without touching attempts.
func TestNextJobReturnsOldestForNode(t *testing.T) {
	now := time.Now().UTC()
	handler, jobs := newJobHandler(t,
		store.Job{ID: "newer", Command: []string{"b"}, Status: store.JobAssigned, NodeID: "node1", Attempts: 1, CreatedAt: now},
		store.Job{ID: "older", Command: []string{"a"}, Status: store.JobAssigned, NodeID: "node1", Attempts: 2, CreatedAt: now.Add(-time.Minute)},
		store.Job{ID: "elsewhere", Command: []string{"c"}, Status: store.JobAssigned, NodeID: "node2", Attempts: 1, CreatedAt: now.Add(-time.Hour)},
	)
	// Request the next job for node1 twice and ensure the oldest assigned job is returned each time.
	for i := 0; i < 2; i++ {
		rec := getJSON(handler, "/nodes/node1/jobs/next")
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d", i, rec.Code)
		}
		var got store.Job
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("call %d: failed to decode: %v", i, err)
		}
		if got.ID != "older" {
			t.Fatalf("call %d: expected job older, got %q", i, got.ID)
		}
		if got.Attempts != 2 {
			t.Fatalf("call %d: expected attempts 2, got %d", i, got.Attempts)
		}
		stored, _ := jobs.Get(context.Background(), "older")
		if stored.Status != store.JobRunning {
			t.Fatalf("call %d: expected stored status running, got %q", i, stored.Status)
		}
	}

	other, _ := jobs.Get(context.Background(), "elsewhere")
	if other.Status != store.JobAssigned {
		t.Fatalf("other node's job changed to %q", other.Status)
	}
}

// TestPostJobResultOutcome checks that the exit code decides done or failed.
func TestPostJobResultOutcome(t *testing.T) {
	cases := []struct {
		body string
		want store.JobStatus
	}{
		{`{"node_id":"node1","attempt":1,"exit_code":0,"stdout":"hi\n"}`, store.JobDone},
		{`{"node_id":"node1","attempt":1,"exit_code":3,"stderr":"boom"}`, store.JobFailed},
	}

	for _, tc := range cases {
		handler, jobs := newJobHandler(t, store.Job{ID: "job1", Status: store.JobRunning, NodeID: "node1", Attempts: 1})
		if code := postResult(handler, "job1", tc.body); code != http.StatusNoContent {
			t.Fatalf("%s: expected 204, got %d", tc.body, code)
		}
		j, _ := jobs.Get(context.Background(), "job1")
		if j.Status != tc.want {
			t.Errorf("%s: expected status %q, got %q", tc.body, tc.want, j.Status)
		}
	}
}

// TestPostJobResultRejected checks bad JSON, a missing exit_code, an
// oversized body and an unknown job.
func TestPostJobResultRejected(t *testing.T) {
	handler, jobs := newJobHandler(t, store.Job{ID: "job1", Status: store.JobRunning, NodeID: "node1", Attempts: 1})
	big := `{"node_id":"node1","attempt":1,"exit_code":0,"stdout":"` + strings.Repeat("a", 16<<20) + `"}`
	cases := []struct {
		name string
		id   string
		body string
		want int
	}{
		{"bad json", "job1", `{`, http.StatusBadRequest},
		{"missing exit_code", "job1", `{"node_id":"node1","attempt":1}`, http.StatusBadRequest},
		{"empty body", "job1", ``, http.StatusBadRequest},
		{"oversized", "job1", big, http.StatusRequestEntityTooLarge},
		{"unknown job", "nope", `{"node_id":"node1","attempt":1,"exit_code":0}`, http.StatusNotFound},
	}

	for _, tc := range cases {
		if code := postResult(handler, tc.id, tc.body); code != tc.want {
			t.Errorf("%s: expected %d, got %d", tc.name, tc.want, code)
		}
	}

	j, _ := jobs.Get(context.Background(), "job1")
	if j.Status != store.JobRunning {
		t.Errorf("rejected results changed job to %q", j.Status)
	}
}

// TestJobOutput checks the output endpoint across the job lifecycle.
func TestJobOutput(t *testing.T) {
	handler, _ := newJobHandler(t,
		store.Job{ID: "queued", Status: store.JobQueued},
		store.Job{ID: "assigned", Status: store.JobAssigned, NodeID: "node1"},
		store.Job{ID: "running", Status: store.JobRunning, NodeID: "node1"},
		store.Job{ID: "reported", Status: store.JobRunning, NodeID: "node1", Attempts: 1},
		store.Job{ID: "gaveup", Status: store.JobFailed, Attempts: 3},
	)

	for _, id := range []string{"queued", "assigned", "running"} {
		if rec := getJSON(handler, "/jobs/"+id+"/output"); rec.Code != http.StatusConflict {
			t.Errorf("%s: expected 409, got %d", id, rec.Code)
		}
	}

	if rec := getJSON(handler, "/jobs/nope/output"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown job: expected 404, got %d", rec.Code)
	}

	type output struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}

	read := func(id string) output {
		t.Helper()
		rec := getJSON(handler, "/jobs/"+id+"/output")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", id, rec.Code)
		}
		var o output
		if err := json.NewDecoder(rec.Body).Decode(&o); err != nil {
			t.Fatalf("%s: failed to decode: %v", id, err)
		}
		return o
	}

	body := `{"node_id":"node1","attempt":1,"exit_code":3,"stdout":"hi\n","stderr":"oops\n"}`
	if code := postResult(handler, "reported", body); code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", code)
	}

	if got, want := read("reported"), (output{"hi\n", "oops\n", 3}); got != want {
		t.Errorf("reported: expected %+v, got %+v", want, got)
	}

	if got, want := read("gaveup"), (output{"", "", -1}); got != want {
		t.Errorf("gaveup: expected %+v, got %+v", want, got)
	}
}

// TestJobResponsesOmitOutput checks that stdout and stderr stay out of the
// job endpoints the dashboard polls.
func TestJobResponsesOmitOutput(t *testing.T) {
	handler, _ := newJobHandler(t, store.Job{ID: "job1", Status: store.JobFailed, ExitCode: 1, Stdout: "SECRETOUT", Stderr: "SECRETERR"})
	for _, path := range []string{"/jobs", "/jobs/job1"} {
		body := getJSON(handler, path).Body.String()
		if strings.Contains(body, "SECRET") {
			t.Errorf("%s leaked output: %s", path, body)
		}
	}
}

// TestNodeListStatus checks that GET /nodes reports online for a
// recently seen node and offline for one last seen 60 seconds ago.
func TestNodeListStatus(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	createTestPool(t, pools)
	handler := NewHandler(reg, pools, store.NewMemJobs(), store.NewMemCredits(), testServer())

	fresh := store.Node{ID: "fresh", PoolID: "test-pool", LastSeen: time.Now().UTC()}
	old := store.Node{ID: "old", PoolID: "test-pool", LastSeen: time.Now().UTC().Add(-60 * time.Second)}

	for _, n := range []store.Node{fresh, old} {
		if err := reg.Upsert(context.Background(), n); err != nil {
			t.Fatalf("failed to upsert node: %v", err)
		}
	}

	rec := getJSON(handler, "/nodes")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var got []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	status := map[string]string{}
	for _, n := range got {
		status[n.ID] = n.Status
	}

	if status["fresh"] != "online" {
		t.Errorf("expected fresh node online, got %q", status["fresh"])
	}
	if status["old"] != "offline" {
		t.Errorf("expected old node offline, got %q", status["old"])
	}
}

// newLeaveHandler sets up two pools with one node each and the given jobs.
// Pool p1's token is "tok1" and its node is n1; pool p2's are "tok2" and n2.
func newLeaveHandler(t *testing.T, js ...store.Job) (http.Handler, store.Registry, store.JobStore) {
	t.Helper()
	ctx := context.Background()

	pools := store.NewMemPools()
	for _, p := range []store.Pool{{ID: "p1", Name: "one", Token: "tok1"}, {ID: "p2", Name: "two", Token: "tok2"}} {
		if err := pools.Create(ctx, p); err != nil {
			t.Fatalf("failed to create pool: %v", err)
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

	return NewHandler(reg, pools, jobs, store.NewMemCredits(), testServer()), reg, jobs
}

// deleteNode sends DELETE /nodes/{id} with the given bearer token ("" for
// none) and returns the status code.
func deleteNode(handler http.Handler, id, token string) int {
	req := httptest.NewRequest(http.MethodDelete, "/nodes/"+id, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

func nodeIDs(t *testing.T, reg store.Registry) map[string]bool {
	t.Helper()
	nodes, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("failed to list nodes: %v", err)
	}
	ids := map[string]bool{}
	for _, n := range nodes {
		ids[n.ID] = true
	}
	return ids
}

// TestLeaveRequeuesJobsAndRemovesNode checks that leaving deletes the node,
// puts its unfinished jobs back in the queue with the attempt given back, and
// leaves every other job alone.
func TestLeaveRequeuesJobsAndRemovesNode(t *testing.T) {
	handler, reg, jobs := newLeaveHandler(t,
		store.Job{ID: "running", Command: []string{"a"}, Status: store.JobRunning, NodeID: "n1", Attempts: 1},
		store.Job{ID: "assigned", Command: []string{"b"}, Status: store.JobAssigned, NodeID: "n1", Attempts: 2},
		store.Job{ID: "done", Command: []string{"c"}, Status: store.JobDone, NodeID: "n1", Attempts: 1},
		store.Job{ID: "other", Command: []string{"d"}, Status: store.JobRunning, NodeID: "n2", Attempts: 1},
	)

	if code := deleteNode(handler, "n1", "tok1"); code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", code)
	}

	if ids := nodeIDs(t, reg); ids["n1"] || !ids["n2"] {
		t.Fatalf("expected only n2 left, got %v", ids)
	}

	want := map[string]struct {
		status   store.JobStatus
		node     string
		attempts int
	}{
		"running":  {store.JobQueued, "", 0},
		"assigned": {store.JobQueued, "", 1},
		"done":     {store.JobDone, "n1", 1},
		"other":    {store.JobRunning, "n2", 1},
	}
	for id, w := range want {
		j, err := jobs.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("failed to get job %s: %v", id, err)
		}
		if j.Status != w.status || j.NodeID != w.node || j.Attempts != w.attempts {
			t.Errorf("job %s: got status=%s node=%q attempts=%d, want status=%s node=%q attempts=%d",
				id, j.Status, j.NodeID, j.Attempts, w.status, w.node, w.attempts)
		}
	}
}

// TestLeaveRejected checks that a missing or wrong token, or a token for
// another pool, gets 403 and changes nothing, and that an unknown node gets 404.
func TestLeaveRejected(t *testing.T) {
	running := store.Job{ID: "running", Command: []string{"a"}, Status: store.JobRunning, NodeID: "n1", Attempts: 1}

	cases := []struct {
		name  string
		node  string
		token string
		want  int
	}{
		{"no token", "n1", "", http.StatusForbidden},
		{"unknown token", "n1", "nope", http.StatusForbidden},
		{"other pool's token", "n1", "tok2", http.StatusForbidden},
		{"unknown node", "ghost", "tok1", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handler, reg, jobs := newLeaveHandler(t, running)

			if code := deleteNode(handler, c.node, c.token); code != c.want {
				t.Fatalf("expected %d, got %d", c.want, code)
			}

			if ids := nodeIDs(t, reg); !ids["n1"] || !ids["n2"] {
				t.Errorf("expected both nodes to stay, got %v", ids)
			}
			j, _ := jobs.Get(context.Background(), "running")
			if j.Status != store.JobRunning || j.NodeID != "n1" || j.Attempts != 1 {
				t.Errorf("expected job unchanged, got %+v", j)
			}
		})
	}
}

// chargeFixture is a running job in pool p1 by "akrm" that started
// `ago` before now, with the ledger it will be charged to.
func chargeFixture(t *testing.T, ago time.Duration) (http.Handler, store.JobStore, store.CreditStore) {
	t.Helper()
	started := time.Now().UTC().Add(-ago)
	j := store.Job{ID: "job1", Command: []string{"sleep"}, Status: store.JobRunning, NodeID: "n1",
		PoolID: "p1", Submitter: "akrm", Attempts: 1, StartedAt: &started}
	jobs := store.NewMemJobs()
	if err := jobs.Create(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	credits := store.NewMemCredits()
	pools := store.NewMemPools()
	if err := pools.Create(context.Background(), store.Pool{ID: "p1", Name: "one", Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	return NewHandler(store.NewMem(), pools, jobs, credits, testServer()), jobs, credits
}

// TestResultChargesSubmitter checks that an accepted result stamps
// finished_at and bills the submitter 1 credit per started minute, tagged
// with the job ID, and that a second report is 409 and charges nothing more.
func TestResultChargesSubmitter(t *testing.T) {
	handler, jobs, credits := chargeFixture(t, 90*time.Second)

	if code := postResult(handler, "job1", `{"node_id":"n1","attempt":1,"exit_code":0}`); code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", code)
	}
	j, _ := jobs.Get(context.Background(), "job1")
	if j.FinishedAt == nil || j.StartedAt == nil {
		t.Fatalf("expected both timestamps set, got started=%v finished=%v", j.StartedAt, j.FinishedAt)
	}
	balances, _ := credits.Balances(context.Background(), "p1")
	if balances["akrm"] != -2 {
		t.Errorf("expected akrm charged 2, got balance %d", balances["akrm"])
	}

	if code := postResult(handler, "job1", `{"node_id":"n1","attempt":1,"exit_code":0}`); code != http.StatusConflict {
		t.Fatalf("second result: expected 409, got %d", code)
	}
	balances, _ = credits.Balances(context.Background(), "p1")
	if balances["akrm"] != -2 {
		t.Errorf("second result changed the balance to %d", balances["akrm"])
	}
}

// TestNextJobSetsStartedAt checks that picking up an assigned job starts
// its clock, and that a second pickup of the running job doesn't restart it.
func TestNextJobSetsStartedAt(t *testing.T) {
	handler, jobs := newJobHandler(t, store.Job{ID: "job1", Command: []string{"x"}, Status: store.JobAssigned, NodeID: "n1", Attempts: 1})

	if rec := getJSON(handler, "/nodes/n1/jobs/next"); rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	first, _ := jobs.Get(context.Background(), "job1")
	if first.StartedAt == nil {
		t.Fatal("expected started_at to be set after pickup")
	}

	getJSON(handler, "/nodes/n1/jobs/next")
	second, _ := jobs.Get(context.Background(), "job1")
	if !second.StartedAt.Equal(*first.StartedAt) {
		t.Errorf("started_at moved on second pickup: %v -> %v", first.StartedAt, second.StartedAt)
	}
}
