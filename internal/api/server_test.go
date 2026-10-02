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
	NewHandler(reg, pools, store.NewMemJobs()).ServeHTTP(rec, req)

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
	handler := NewHandler(reg, pools, store.NewMemJobs())

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

	// Request only the nodes belonging to this pool.
	req = httptest.NewRequest(
		http.MethodGet,
		"/nodes?pool="+pool.ID,
		nil,
	)

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

	// Request nodes from a different pool ID.
	req = httptest.NewRequest(
		http.MethodGet,
		"/nodes?pool=other",
		nil,
	)

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
	handler := NewHandler(reg, pools, store.NewMemJobs())

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
	handler := NewHandler(reg, pools, store.NewMemJobs())

	req := httptest.NewRequest(
		http.MethodGet,
		"/nodes",
		nil,
	)

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
	handler := NewHandler(reg, pools, store.NewMemJobs())

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

// TestNodeHeartbeatInvalidPoolToken checks that a heartbeat
// with an invalid pool token is rejected with 403 Forbidden.
func TestNodeHeartbeatInvalidPoolToken(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	handler := NewHandler(reg, pools, store.NewMemJobs())

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

// TestJobCreate checks that a job can be submitted
// and is stored with a generated ID and queued status.
func TestJobCreate(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs)

	// Submit a new job.
	body := `{
		"command":["echo","hi"]
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/jobs",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Creating a job should return 201 Created.
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	// Decode the response returned by POST /jobs.
	var response struct {
		ID     string          `json:"id"`
		Status store.JobStatus `json:"status"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode job response: %v", err)
	}

	// Make sure the server generated a 12-character job ID.
	if len(response.ID) != 12 {
		t.Errorf("expected job ID length 12, got %d", len(response.ID))
	}

	// New jobs should always start in the queued state.
	if response.Status != store.JobQueued {
		t.Errorf("expected status %q, got %q", store.JobQueued, response.Status)
	}

	// Make sure the job was actually saved in the job store.
	created, err := jobs.Get(req.Context(), response.ID)
	if err != nil {
		t.Fatalf("failed to get created job from store: %v", err)
	}

	// Make sure the submitted command was stored correctly.
	if len(created.Command) != 2 ||
		created.Command[0] != "echo" ||
		created.Command[1] != "hi" {
		t.Errorf("expected command [echo hi], got %v", created.Command)
	}

	// The server should assign the queued status.
	if created.Status != store.JobQueued {
		t.Errorf("expected stored status %q, got %q", store.JobQueued, created.Status)
	}

	// The server should assign a creation time.
	if created.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

// TestJobCreateEmptyCommand checks that a job without a command
// is rejected with HTTP 400 Bad Request.
func TestJobCreateEmptyCommand(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs)

	body := `{
		"command":[]
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/jobs",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

// TestJobGet checks that a submitted job can be retrieved by its ID.
func TestJobGet(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs)

	// Create a job first.
	body := `{
		"command":["echo","hi"]
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/jobs",
		strings.NewReader(body),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	// Decode the ID returned when the job was created.
	var created struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode job response: %v", err)
	}

	// Retrieve the job using its ID.
	req = httptest.NewRequest(
		http.MethodGet,
		"/jobs/"+created.ID,
		nil,
	)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var job store.Job

	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("failed to decode job: %v", err)
	}

	if job.ID != created.ID {
		t.Errorf("expected job ID %q, got %q", created.ID, job.ID)
	}

	if len(job.Command) != 2 ||
		job.Command[0] != "echo" ||
		job.Command[1] != "hi" {
		t.Errorf("expected command [echo hi], got %v", job.Command)
	}

	if job.Status != store.JobQueued {
		t.Errorf("expected status %q, got %q", store.JobQueued, job.Status)
	}
}

// TestJobGetNotFound checks that requesting an unknown job
// returns HTTP 404 Not Found.
func TestJobGetNotFound(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs)

	req := httptest.NewRequest(
		http.MethodGet,
		"/jobs/doesnotexist",
		nil,
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

// TestJobList checks that submitted jobs can be retrieved
// from the job list endpoint.
func TestJobList(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs)

	// Create the first job.
	body := `{
		"command":["echo","first"]
	}`

	req := httptest.NewRequest(
		http.MethodPost,
		"/jobs",
		strings.NewReader(body),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	// Create the second job.
	body = `{
		"command":["echo","second"]
	}`

	req = httptest.NewRequest(
		http.MethodPost,
		"/jobs",
		strings.NewReader(body),
	)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", rec.Code)
	}

	// Request the list of jobs.
	req = httptest.NewRequest(
		http.MethodGet,
		"/jobs",
		nil,
	)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Decode the returned jobs.
	var listed []store.Job

	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("failed to decode job list: %v", err)
	}

	// We created two jobs.
	if len(listed) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(listed))
	}

	// Make sure the first command was stored correctly.
	// Make sure both submitted commands appear in the list.
	foundFirst := false
	foundSecond := false

	for _, job := range listed {
		if len(job.Command) == 2 &&
			job.Command[0] == "echo" &&
			job.Command[1] == "first" {
			foundFirst = true
		}

		if len(job.Command) == 2 &&
			job.Command[0] == "echo" &&
			job.Command[1] == "second" {
			foundSecond = true
		}
	}

	if !foundFirst {
		t.Error("expected to find command [echo first]")
	}

	if !foundSecond {
		t.Error("expected to find command [echo second]")
	}
}

// TestJobListEmpty checks that a fresh job store returns
// an empty JSON array instead of null.
func TestJobListEmpty(t *testing.T) {
	reg := store.NewMem()
	pools := store.NewMemPools()
	jobs := store.NewMemJobs()
	handler := NewHandler(reg, pools, jobs)

	req := httptest.NewRequest(
		http.MethodGet,
		"/jobs",
		nil,
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("expected body [], got %s", rec.Body.String())
	}
}

// seedJob stores a job directly so a test can set fields the API doesn't
// let a client choose, like its status, node and creation time.
func seedJob(t *testing.T, jobs store.JobStore, j store.Job) {
	t.Helper()

	if j.Command == nil {
		j.Command = []string{"echo", "hi"}
	}

	if err := jobs.Create(context.Background(), j); err != nil {
		t.Fatalf("failed to seed job %q: %v", j.ID, err)
	}
}

// send runs one request through the handler and returns the recorded response.
func send(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// mustGetJob reads a job back from the store.
func mustGetJob(t *testing.T, jobs store.JobStore, id string) store.Job {
	t.Helper()

	j, err := jobs.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("failed to get job %q: %v", id, err)
	}
	return j
}

// TestJobsNextNothingAssigned checks that a node with no work gets
// 204 No Content, whether or not the registry knows the node.
func TestJobsNextNothingAssigned(t *testing.T) {
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

	// No jobs at all, and a node that was never registered.
	rec := send(handler, http.MethodGet, "/nodes/node-a/jobs/next", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}

	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", rec.Body.String())
	}

	// Jobs that are queued, finished, or on another node are not this node's work.
	now := time.Now().UTC()
	seedJob(t, jobs, store.Job{ID: "queued", Status: store.JobQueued, CreatedAt: now})
	seedJob(t, jobs, store.Job{ID: "done", Status: store.JobDone, NodeID: "node-a", CreatedAt: now})
	seedJob(t, jobs, store.Job{ID: "failed", Status: store.JobFailed, NodeID: "node-a", CreatedAt: now})
	seedJob(t, jobs, store.Job{ID: "other", Status: store.JobAssigned, NodeID: "node-b", CreatedAt: now})

	rec = send(handler, http.MethodGet, "/nodes/node-a/jobs/next", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}

	// Asking must not have touched anyone's job.
	if got := mustGetJob(t, jobs, "other"); got.Status != store.JobAssigned {
		t.Errorf("expected other node's job to stay %q, got %q", store.JobAssigned, got.Status)
	}
}

// TestJobsNextReturnsOldestForNode checks that a node gets its oldest
// unfinished job, never another node's, that the job is marked running,
// and that asking again returns the same job without touching attempts.
func TestJobsNextReturnsOldestForNode(t *testing.T) {
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

	base := time.Now().UTC().Truncate(time.Second)

	// Seeded newest first, so insertion order can't be mistaken for age.
	seedJob(t, jobs, store.Job{ID: "a-newer", Status: store.JobAssigned, NodeID: "node-a", Attempts: 1, CreatedAt: base.Add(3 * time.Second)})
	seedJob(t, jobs, store.Job{ID: "a-oldest", Status: store.JobAssigned, NodeID: "node-a", Attempts: 2, CreatedAt: base.Add(2 * time.Second)})
	seedJob(t, jobs, store.Job{ID: "b-older", Status: store.JobAssigned, NodeID: "node-b", Attempts: 1, CreatedAt: base.Add(1 * time.Second)})
	seedJob(t, jobs, store.Job{ID: "a-finished", Status: store.JobDone, NodeID: "node-a", Attempts: 1, CreatedAt: base})

	for call := 1; call <= 2; call++ {
		rec := send(handler, http.MethodGet, "/nodes/node-a/jobs/next", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected status 200, got %d", call, rec.Code)
		}

		var got store.Job
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("call %d: failed to decode job: %v", call, err)
		}

		if got.ID != "a-oldest" {
			t.Fatalf("call %d: expected job %q, got %q", call, "a-oldest", got.ID)
		}

		if got.Status != store.JobRunning {
			t.Errorf("call %d: expected response status %q, got %q", call, store.JobRunning, got.Status)
		}

		if got.NodeID != "node-a" {
			t.Errorf("call %d: expected node_id %q, got %q", call, "node-a", got.NodeID)
		}

		if got.Attempts != 2 {
			t.Errorf("call %d: expected attempts 2, got %d", call, got.Attempts)
		}

		if len(got.Command) != 2 || got.Command[0] != "echo" || got.Command[1] != "hi" {
			t.Errorf("call %d: expected command [echo hi], got %v", call, got.Command)
		}

		stored := mustGetJob(t, jobs, "a-oldest")
		if stored.Status != store.JobRunning {
			t.Errorf("call %d: expected stored status %q, got %q", call, store.JobRunning, stored.Status)
		}

		if stored.Attempts != 2 {
			t.Errorf("call %d: expected stored attempts 2, got %d", call, stored.Attempts)
		}
	}

	// Only the job that was handed out changes.
	if got := mustGetJob(t, jobs, "a-newer"); got.Status != store.JobAssigned {
		t.Errorf("expected %q to stay %q, got %q", "a-newer", store.JobAssigned, got.Status)
	}

	if got := mustGetJob(t, jobs, "b-older"); got.Status != store.JobAssigned {
		t.Errorf("expected %q to stay %q, got %q", "b-older", store.JobAssigned, got.Status)
	}

	// Once the first job reports, the node moves on to its next one.
	rec := send(handler, http.MethodPost, "/jobs/a-oldest/result", `{"exit_code":0}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected result status 204, got %d", rec.Code)
	}

	rec = send(handler, http.MethodGet, "/nodes/node-a/jobs/next", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var next store.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &next); err != nil {
		t.Fatalf("failed to decode job: %v", err)
	}

	if next.ID != "a-newer" || next.Status != store.JobRunning {
		t.Errorf("expected %q running, got %q %q", "a-newer", next.ID, next.Status)
	}
}

// TestJobsNextHidesOutput checks that the job a node picks up and the job
// endpoints never carry stdout or stderr.
func TestJobsNextHidesOutput(t *testing.T) {
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

	seedJob(t, jobs, store.Job{
		ID:        "j1",
		Status:    store.JobRunning,
		NodeID:    "node-a",
		Stdout:    "secret-out",
		Stderr:    "secret-err",
		CreatedAt: time.Now().UTC(),
	})

	for _, path := range []string{"/nodes/node-a/jobs/next", "/jobs/j1", "/jobs"} {
		rec := send(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected status 200, got %d", path, rec.Code)
		}

		body := rec.Body.String()
		for _, leaked := range []string{"stdout", "stderr", "secret-out", "secret-err"} {
			if strings.Contains(body, leaked) {
				t.Errorf("%s: expected body to omit %q, got %s", path, leaked, body)
			}
		}
	}
}

// TestJobResult checks that a reported result is stored and the job
// finishes as done for exit code 0 and failed for anything else.
func TestJobResult(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus store.JobStatus
		wantCode   int
	}{
		{"zero is done", `{"exit_code":0,"stdout":"hi\n","stderr":"warn\n"}`, store.JobDone, 0},
		{"non-zero is failed", `{"exit_code":3,"stdout":"hi\n","stderr":"warn\n"}`, store.JobFailed, 3},
		{"negative is failed", `{"exit_code":-1,"stdout":"hi\n","stderr":"warn\n"}`, store.JobFailed, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := store.NewMemJobs()
			handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

			seedJob(t, jobs, store.Job{
				ID:        "j1",
				Status:    store.JobRunning,
				NodeID:    "node-a",
				Attempts:  2,
				CreatedAt: time.Now().UTC(),
			})

			rec := send(handler, http.MethodPost, "/jobs/j1/result", tt.body)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("expected status 204, got %d", rec.Code)
			}

			got := mustGetJob(t, jobs, "j1")

			if got.Status != tt.wantStatus {
				t.Errorf("expected status %q, got %q", tt.wantStatus, got.Status)
			}

			if got.ExitCode != tt.wantCode {
				t.Errorf("expected exit code %d, got %d", tt.wantCode, got.ExitCode)
			}

			if got.Stdout != "hi\n" || got.Stderr != "warn\n" {
				t.Errorf("expected stdout %q and stderr %q, got %q and %q", "hi\n", "warn\n", got.Stdout, got.Stderr)
			}

			// Reporting a result must not disturb the rest of the job.
			if got.NodeID != "node-a" || got.Attempts != 2 {
				t.Errorf("expected node-a and attempts 2, got %q and %d", got.NodeID, got.Attempts)
			}
		})
	}
}

// TestJobResultBadRequests checks that a bad body is rejected and leaves the
// job alone, and that an unknown job is a 404.
func TestJobResultBadRequests(t *testing.T) {
	// The largest body the cap lets through, and one byte more.
	const prefix = `{"exit_code":0,"stdout":"`
	const suffix = `"}`
	fill := maxResultBytes - len(prefix) - len(suffix)
	atLimit := prefix + strings.Repeat("a", fill) + suffix
	overLimit := prefix + strings.Repeat("a", fill+1) + suffix

	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{"bad json", "/jobs/j1/result", `not json`, http.StatusBadRequest},
		{"empty body", "/jobs/j1/result", ``, http.StatusBadRequest},
		{"missing exit_code", "/jobs/j1/result", `{"stdout":"hi\n","stderr":""}`, http.StatusBadRequest},
		{"null exit_code", "/jobs/j1/result", `{"exit_code":null}`, http.StatusBadRequest},
		{"non-integer exit_code", "/jobs/j1/result", `{"exit_code":"0"}`, http.StatusBadRequest},
		{"trailing garbage", "/jobs/j1/result", `{"exit_code":0} junk`, http.StatusBadRequest},
		{"oversized", "/jobs/j1/result", overLimit, http.StatusRequestEntityTooLarge},
		{"oversized and not json", "/jobs/j1/result", strings.Repeat("a", maxResultBytes+1), http.StatusRequestEntityTooLarge},
		{"unknown job", "/jobs/nope/result", `{"exit_code":0}`, http.StatusNotFound},
		{"exactly at the limit", "/jobs/j1/result", atLimit, http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := store.NewMemJobs()
			handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

			seedJob(t, jobs, store.Job{
				ID:        "j1",
				Status:    store.JobRunning,
				NodeID:    "node-a",
				CreatedAt: time.Now().UTC(),
			})

			rec := send(handler, http.MethodPost, tt.path, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("expected status %d, got %d", tt.want, rec.Code)
			}

			// Anything but a stored result leaves the job running.
			if tt.want != http.StatusNoContent {
				if got := mustGetJob(t, jobs, "j1"); got.Status != store.JobRunning {
					t.Errorf("expected job to stay %q, got %q", store.JobRunning, got.Status)
				}
			}
		})
	}
}

// TestJobResultEscapedStreams checks that two 1 MiB streams still fit the
// body cap when every byte JSON-escapes to six, and come back intact.
func TestJobResultEscapedStreams(t *testing.T) {
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

	seedJob(t, jobs, store.Job{ID: "j1", Status: store.JobRunning, NodeID: "node-a", CreatedAt: time.Now().UTC()})

	stream := strings.Repeat("\x00", 1<<20)

	body, err := json.Marshal(map[string]any{"exit_code": 0, "stdout": stream, "stderr": stream})
	if err != nil {
		t.Fatalf("failed to encode result: %v", err)
	}

	if len(body) < 12<<20 {
		t.Fatalf("expected a body of at least 12 MiB, got %d bytes", len(body))
	}

	rec := send(handler, http.MethodPost, "/jobs/j1/result", string(body))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}

	got := mustGetJob(t, jobs, "j1")
	if got.Stdout != stream || got.Stderr != stream {
		t.Errorf("expected both streams to round-trip, got %d and %d bytes", len(got.Stdout), len(got.Stderr))
	}
}

// TestJobOutput checks that output is a 409 until the job finishes, then
// returns exactly what the node reported.
func TestJobOutput(t *testing.T) {
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

	now := time.Now().UTC()
	seedJob(t, jobs, store.Job{ID: "queued", Status: store.JobQueued, CreatedAt: now})
	seedJob(t, jobs, store.Job{ID: "assigned", Status: store.JobAssigned, NodeID: "node-a", CreatedAt: now})
	seedJob(t, jobs, store.Job{ID: "running", Status: store.JobRunning, NodeID: "node-a", CreatedAt: now})

	for _, id := range []string{"queued", "assigned", "running"} {
		rec := send(handler, http.MethodGet, "/jobs/"+id+"/output", "")
		if rec.Code != http.StatusConflict {
			t.Errorf("%s: expected status 409, got %d", id, rec.Code)
		}
	}

	rec := send(handler, http.MethodGet, "/jobs/nope/output", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown job: expected status 404, got %d", rec.Code)
	}

	tests := []struct {
		id       string
		body     string
		stdout   string
		stderr   string
		exitCode int
	}{
		{"running", `{"exit_code":0,"stdout":"hi\n","stderr":""}`, "hi\n", "", 0},
		{"assigned", `{"exit_code":3,"stdout":"partial\n","stderr":"boom\n"}`, "partial\n", "boom\n", 3},
	}

	for _, tt := range tests {
		rec := send(handler, http.MethodPost, "/jobs/"+tt.id+"/result", tt.body)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: expected result status 204, got %d", tt.id, rec.Code)
		}

		rec = send(handler, http.MethodGet, "/jobs/"+tt.id+"/output", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected output status 200, got %d", tt.id, rec.Code)
		}

		var out struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode *int   `json:"exit_code"`
		}

		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: failed to decode output: %v", tt.id, err)
		}

		if out.Stdout != tt.stdout || out.Stderr != tt.stderr {
			t.Errorf("%s: expected stdout %q and stderr %q, got %q and %q", tt.id, tt.stdout, tt.stderr, out.Stdout, out.Stderr)
		}

		if out.ExitCode == nil || *out.ExitCode != tt.exitCode {
			t.Errorf("%s: expected exit_code %d, got %v", tt.id, tt.exitCode, out.ExitCode)
		}
	}
}

// TestJobOutputSchedulerFailed checks that a job the scheduler gave up on
// has no output to show, and reports exit code -1 so `relay logs` exits
// non-zero instead of exiting 0 as if it had succeeded.
func TestJobOutputSchedulerFailed(t *testing.T) {
	jobs := store.NewMemJobs()
	handler := NewHandler(store.NewMem(), store.NewMemPools(), jobs)

	// This is what the scheduler leaves behind: failed, no node, exit code 0.
	seedJob(t, jobs, store.Job{
		ID:        "lost",
		Status:    store.JobFailed,
		Attempts:  3,
		CreatedAt: time.Now().UTC(),
	})

	rec := send(handler, http.MethodGet, "/jobs/lost/output", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var out struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode *int   `json:"exit_code"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}

	if out.Stdout != "" || out.Stderr != "" {
		t.Errorf("expected empty output, got stdout %q and stderr %q", out.Stdout, out.Stderr)
	}

	if out.ExitCode == nil || *out.ExitCode != -1 {
		t.Errorf("expected exit_code -1, got %v", out.ExitCode)
	}
}
