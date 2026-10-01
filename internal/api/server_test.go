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

// postResult sends a result for a job and returns the response status code.
func postResult(handler http.Handler, jobID, body string) int {
	req := httptest.NewRequest(
		http.MethodPost,
		"/jobs/"+jobID+"/result",
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec.Code
}

// TestPostJobResultStale checks that results from the wrong node,
// an old attempt, or an already-done job are rejected with 409.
func TestPostJobResultStale(t *testing.T) {
	newHandler := func(j store.Job) (http.Handler, store.JobStore) {
		jobs := store.NewMemJobs()
		if err := jobs.Create(context.Background(), j); err != nil {
			t.Fatalf("failed to create job: %v", err)
		}
		return NewHandler(store.NewMem(), store.NewMemPools(), jobs), jobs
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

		code := postResult(handler, "job1", `{"node_id":"node1","attempt":2}`)
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

		code := postResult(handler, "job1", `{"node_id":"node1","attempt":1}`)
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

		code := postResult(handler, "job1", `{"node_id":"node2","attempt":2}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", code)
		}
	})

	t.Run("already done job returns 409", func(t *testing.T) {
		done := assigned
		done.Status = store.JobDone

		handler, _ := newHandler(done)

		code := postResult(handler, "job1", `{"node_id":"node1","attempt":2}`)
		if code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", code)
		}
	})
}

// TestNodeListStatus checks that GET /nodes reports online for a
// recently seen node and offline for one last seen 60 seconds ago.
func TestNodeListStatus(t *testing.T) {
	reg := store.NewMem()
	handler := NewHandler(reg, store.NewMemPools(), store.NewMemJobs())

	fresh := store.Node{ID: "fresh", LastSeen: time.Now().UTC()}
	old := store.Node{ID: "old", LastSeen: time.Now().UTC().Add(-60 * time.Second)}

	for _, n := range []store.Node{fresh, old} {
		if err := reg.Upsert(context.Background(), n); err != nil {
			t.Fatalf("failed to upsert node: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

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