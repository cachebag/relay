package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/cachebag/relay/internal/credit"
	"github.com/cachebag/relay/internal/store"
)

// maxResultBytes caps a results request body size.
const maxResultBytes = 16 << 20

// nodeView is a node plus its online/offline status, as returned by GET /nodes.
type nodeView struct {
	store.Node
	Status string `json:"status"`
}

// NewHandler creates and returns the HTTP handler for the relay API.
func NewHandler(reg store.Registry, pools store.PoolStore, jobs store.JobStore, credits store.CreditStore, server store.ServerStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Creates a new pool. Only the server admin can, so strangers can't make
	// pools on someone else's relayd once it is on the internet.
	mux.HandleFunc("POST /pools", func(w http.ResponseWriter, r *http.Request) {
		if !checkServerAdmin(w, r, server) {
			return
		}

		var req struct {
			Name string `json:"name"`
		}

		// Read the pool name from the request.
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		// Create the pool with a new ID and token.
		p := store.Pool{
			ID:        store.NewID(),
			Name:      req.Name,
			Token:     store.NewToken(),
			CreatedAt: time.Now().UTC(),
		}

		// Save the new pool.
		if err := pools.Create(r.Context(), p); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Generate the admin token and store only its hash.
		adminToken := store.NewToken()
		if err := pools.SetAdminTokenHash(r.Context(), p.ID, store.HashToken(adminToken)); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Return the created pool.
		// This is the only endpoint that returns the pool token or the admin token.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(struct {
			store.Pool
			AdminToken string `json:"admin_token"`
		}{Pool: p, AdminToken: adminToken})
	})

	// Lists all pools. Server admin only, like creating one.
	mux.HandleFunc("GET /pools", func(w http.ResponseWriter, r *http.Request) {
		if !checkServerAdmin(w, r, server) {
			return
		}

		ps, err := pools.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Hide each pool's token before sending the list.
		for i := range ps {
			ps[i].Token = ""
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ps)
	})

	// Returns the pool that owns the bearer token, so the CLI can check a
	// token without registering a node. The token is never echoed back.
	mux.HandleFunc("GET /pools/self", func(w http.ResponseWriter, r *http.Request) {

		// Extract the bearer token from the Authorization header.
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}

		// Look up the pool associated with this token.
		p, err := pools.GetByToken(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Return the pool information without the token.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			ID        string    `json:"id"`
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"created_at"`
		}{p.ID, p.Name, p.CreatedAt})
	})

	// Heartbeat endpoint for nodes to report their status.
	mux.HandleFunc("POST /nodes/heartbeat", func(w http.ResponseWriter, r *http.Request) {

		// Heartbeats now include the normal node information
		// plus the pool token.
		var req struct {
			store.Node
			PoolToken string `json:"pool_token"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		n := req.Node

		if n.ID == "" {
			http.Error(w, "id is required", http.StatusBadRequest)
			return
		}

		// Find the pool that owns this token.
		p, err := pools.GetByToken(r.Context(), req.PoolToken)

		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}

		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// The node does not choose its pool ID.
		// The server gets it from the pool token.
		n.PoolID = p.ID

		n.LastSeen = time.Now().UTC()

		// A machine a pool admin removed gets 410 and nothing is saved,
		// so it can't heartbeat its way back into the pool.
		if refuseRemoved(w, r, reg, p.ID, n.ID) {
			return
		}

		if err := reg.Upsert(r.Context(), n); errors.Is(err, store.ErrPoolConflict) {
			http.Error(w, "node ID already belongs to another pool; leave that pool before joining this one", http.StatusConflict)
			return
		} else if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// Lists the nodes in the bearer token's pool. The token decides the
	// pool, so a member can't list another pool's machines.
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) {
		p, ok := poolFromBearer(w, r, pools)
		if !ok {
			return
		}

		all, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		nodes := make([]store.Node, 0)
		for _, n := range all {
			if n.PoolID == p.ID {
				nodes = append(nodes, n)
			}
		}

		// Mark each node online or offline from its last heartbeat.
		now := time.Now()
		views := make([]nodeView, len(nodes))
		for i, n := range nodes {
			status := "offline"
			if store.Online(n, now) {
				status = "online"
			}
			views[i] = nodeView{
				Node:   n,
				Status: status,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(views)
	})

	// Removes a node from its pool when the node leaves. Its unfinished jobs
	// go back to the queue without counting as an attempt.
	mux.HandleFunc("DELETE /nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		// The join token comes as a bearer token.
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}

		p, err := pools.GetByToken(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		nodes, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		var node *store.Node
		for i := range nodes {
			if nodes[i].ID == id {
				node = &nodes[i]
				break
			}
		}
		if node == nil {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}

		// A token can only remove nodes in its own pool.
		if node.PoolID != p.ID {
			http.Error(w, "token does not match this node's pool", http.StatusForbidden)
			return
		}

		// Requeue before deleting, so a failure halfway leaves the node in
		// place instead of orphaning its jobs.
		all, err := jobs.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		for _, j := range all {
			if j.NodeID != id || (j.Status != store.JobAssigned && j.Status != store.JobRunning) {
				continue
			}
			j.Status = store.JobQueued
			j.NodeID = ""

			// The scheduler counts an attempt each time it places a job, so
			// give this one back. Leaving isn't the job's fault.
			if j.Attempts > 0 {
				j.Attempts--
			}
			if err := jobs.Update(r.Context(), j); err != nil {
				http.Error(w, "store error", http.StatusInternalServerError)
				return
			}
			log.Printf("node %s left: job %s back in the queue (attempt %d)", id, j.ID, j.Attempts)
		}

		// Already gone means another request removed it first, which is fine.
		if err := reg.Delete(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		log.Printf("node %s left pool %s", id, p.ID)

		w.WriteHeader(http.StatusNoContent)
	})

	// Creates a new job in the pool identified by the bearer token.
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {

		// Extract the pool join token from the Authorization header.
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}

		// Find the pool associated with the token.
		p, err := pools.GetByToken(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Read the command and submitter from the request.
		var req struct {
			Command   []string       `json:"command"`
			Submitter string         `json:"submitter"`
			Requires  store.Requires `json:"requires"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if len(req.Command) == 0 {
			http.Error(w, "command is required", http.StatusBadRequest)
			return
		}

		// A job must record who submitted it.
		req.Submitter = strings.TrimSpace(req.Submitter)
		if req.Submitter == "" {
			http.Error(w, "submitter is required", http.StatusBadRequest)
			return
		}

		if !req.Requires.Valid() {
			http.Error(w, "required os must be either linux, darwin, windows, or empty", http.StatusBadRequest)
			return
		}

		// Create the job with a server-generated ID and queued status.
		// The authenticated token determines the pool, not the request body.
		j := store.Job{
			ID:        store.NewID(),
			Command:   req.Command,
			Requires:  req.Requires,
			Status:    store.JobQueued,
			PoolID:    p.ID,
			Submitter: req.Submitter,
			CreatedAt: time.Now().UTC(),
		}

		// Save the new job.
		if err := jobs.Create(r.Context(), j); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(struct {
			ID     string          `json:"id"`
			Status store.JobStatus `json:"status"`
		}{
			ID:     j.ID,
			Status: j.Status,
		})
	})

	// Gets a job by ID. Another pool's job is a 404, the same as a job that
	// doesn't exist, so a token can't tell whether it exists.
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := poolFromBearer(w, r, pools)
		if !ok {
			return
		}

		j, ok := poolJob(w, r, jobs, p.ID)
		if !ok {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(j)
	})

	// Lists the jobs in the bearer token's pool.
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, r *http.Request) {
		p, ok := poolFromBearer(w, r, pools)
		if !ok {
			return
		}

		all, err := jobs.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		jobsList := make([]store.Job, 0)
		for _, j := range all {
			if j.PoolID == p.ID {
				jobsList = append(jobsList, j)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobsList)
	})

	// Hands a node its oldest assigned or running job. Handing back a running
	// job means a node that restarted mid-job gets its work back.
	mux.HandleFunc("GET /nodes/{id}/jobs/next", func(w http.ResponseWriter, r *http.Request) {
		p, ok := poolFromBearer(w, r, pools)
		if !ok {
			return
		}

		// Only a node in the token's pool can take that node's jobs.
		// Anyone else gets 403 whether or not the node exists.
		nodeID := r.PathValue("id")
		if !nodeInPool(w, r, reg, nodeID, p.ID) {
			return
		}

		// Get the next job for the node, prioritizing the oldest assigned or running job.
		all, err := jobs.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// List is ordered oldest first, so the first match is the oldest job.
		for _, j := range all {
			if j.NodeID != nodeID || (j.Status != store.JobAssigned && j.Status != store.JobRunning) {
				continue
			}
			if j.Status == store.JobAssigned {
				j.Status = store.JobRunning

				// The clock for this attempt starts now.
				now := time.Now().UTC()
				j.StartedAt = &now
				if err := jobs.Update(r.Context(), j); err != nil {
					http.Error(w, "store error", http.StatusInternalServerError)
					return
				}
			}

			// Return the job to the node.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(j)
			return
		}

		// No job found for the node.
		w.WriteHeader(http.StatusNoContent)
	})

	// Accepts a job result from the node running it.
	mux.HandleFunc("POST /jobs/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		p, ok := poolFromBearer(w, r, pools)
		if !ok {
			return
		}

		id := r.PathValue("id")

		// ExitCode is a pointer so an empty body can't read as success.
		// NodeID and Attempt are required.
		var req struct {
			NodeID   string `json:"node_id"`
			Attempt  int    `json:"attempt"`
			ExitCode *int   `json:"exit_code"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		}

		// Read the node ID and attempt number from the request.
		r.Body = http.MaxBytesReader(w, r.Body, maxResultBytes)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, "result too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if req.ExitCode == nil {
			http.Error(w, "exit_code is required", http.StatusBadRequest)
			return
		}

		j, err := jobs.Get(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}

		// Check for other errors from the store.
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// A token can only report results for its own pool's jobs.
		if j.PoolID != p.ID {
			http.Error(w, "token does not match this job's pool", http.StatusForbidden)
			return
		}

		// Reject the result if the job is no longer active on this node
		// for this attempt. It was requeued or reassigned.
		if (j.Status != store.JobAssigned && j.Status != store.JobRunning) ||
			j.NodeID != req.NodeID || j.Attempts != req.Attempt {
			log.Printf("stale result for job %s from %s (attempt %d, current %d)", j.ID, req.NodeID, req.Attempt, j.Attempts)
			http.Error(w, "stale result: job was reassigned", http.StatusConflict)
			return
		}

		// Mark the job done.
		j.ExitCode = *req.ExitCode
		j.Stdout = req.Stdout
		j.Stderr = req.Stderr
		if j.ExitCode == 0 {
			j.Status = store.JobDone
		} else {
			j.Status = store.JobFailed
		}
		now := time.Now().UTC()
		j.FinishedAt = &now
		if err := jobs.Update(r.Context(), j); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Bill the submitter for the run. The 409 above means a job can
		// only get here once, so it can't be charged twice.
		if err := credit.Charge(r.Context(), credits, j); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// Serves the output a finished job reported.
	mux.HandleFunc("GET /jobs/{id}/output", func(w http.ResponseWriter, r *http.Request) {
		p, ok := poolFromBearer(w, r, pools)
		if !ok {
			return
		}

		// Another pool's job is a 404, the same as GET /jobs/{id}.
		j, ok := poolJob(w, r, jobs, p.ID)
		if !ok {
			return
		}

		// Ensure the job has finished before serving its output.
		if j.Status != store.JobDone && j.Status != store.JobFailed {
			http.Error(w, "job has not finished", http.StatusConflict)
			return
		}

		// A reported failure always has a non-zero exit code, so a failed job
		// with exit code 0 was failed by the scheduler and never reported.
		// Report -1 so relay logs exits non-zero.
		exitCode := j.ExitCode
		if j.Status == store.JobFailed && exitCode == 0 {
			exitCode = -1
		}

		// Serve the job output as JSON.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
		}{
			Stdout:   j.Stdout,
			Stderr:   j.Stderr,
			ExitCode: exitCode,
		})
	})

	// Rotates a pool's join token. Requires the pool's admin token. The old
	// join token stops matching as soon as SetToken returns.
	mux.HandleFunc("POST /pools/{id}/token", func(w http.ResponseWriter, r *http.Request) {
		p, err := pools.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "pool not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		hash, err := pools.AdminTokenHash(r.Context(), p.ID)
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		if !checkAdmin(r, hash) {
			http.Error(w, "invalid admin token", http.StatusUnauthorized)
			return
		}

		newToken := store.NewToken()
		if err := pools.SetToken(r.Context(), p.ID, newToken); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Token string `json:"token"`
		}{Token: newToken})
	})

	// Returns every account's credit balance in the pool, highest first.
	// Anyone holding the pool's join token can see them.
	mux.HandleFunc("GET /pools/{id}/credits", func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}

		p, err := pools.GetByToken(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// A token can only read its own pool's balances.
		if p.ID != r.PathValue("id") {
			http.Error(w, "token does not match this pool", http.StatusForbidden)
			return
		}

		balances, err := credits.Balances(r.Context(), p.ID)
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		type balance struct {
			Account string `json:"account"`
			Balance int    `json:"balance"`
		}
		out := make([]balance, 0, len(balances))
		for account, b := range balances {
			out = append(out, balance{account, b})
		}

		// Highest balance first, then by name so ties don't shuffle.
		sort.Slice(out, func(i, j int) bool {
			if out[i].Balance != out[j].Balance {
				return out[i].Balance > out[j].Balance
			}
			return out[i].Account < out[j].Account
		})

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})

	// Lets a pool admin remove any node from the pool. Requires the
	// pool's admin token. See removenode.go.
	mux.HandleFunc("DELETE /pools/{id}/nodes/{node_id}", handleRemoveNode(reg, pools, jobs))

	return mux
}

// checkAdmin reports whether the Bearer token matches the stored admin token hash.
// An empty hash (pool created before admin tokens) never matches.
func checkAdmin(r *http.Request, hash string) bool {
	if hash == "" {
		return false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return false
	}
	got := store.HashToken(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
}

// poolFromBearer finds the pool that owns the request's bearer join token.
// On a missing or unknown token it answers 403 and returns false, and on a
// store error it answers 500 and returns false.
func poolFromBearer(w http.ResponseWriter, r *http.Request, pools store.PoolStore) (store.Pool, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		http.Error(w, "invalid pool token", http.StatusForbidden)
		return store.Pool{}, false
	}
	p, err := pools.GetByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "invalid pool token", http.StatusForbidden)
		return store.Pool{}, false
	}
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return store.Pool{}, false
	}
	return p, true
}

// poolJob loads the job in the path and checks it belongs to poolID. A job
// in another pool gets the same 404 as a missing one.
func poolJob(w http.ResponseWriter, r *http.Request, jobs store.JobStore, poolID string) (store.Job, bool) {
	j, err := jobs.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || (err == nil && j.PoolID != poolID) {
		http.Error(w, "job not found", http.StatusNotFound)
		return store.Job{}, false
	}
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return store.Job{}, false
	}
	return j, true
}

// nodeInPool answers 403 and returns false unless nodeID is registered in
// poolID. An unknown node gets the same 403, so the route doesn't reveal
// which node IDs exist.
func nodeInPool(w http.ResponseWriter, r *http.Request, reg store.Registry, nodeID, poolID string) bool {
	nodes, err := reg.List(r.Context())
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return false
	}
	for _, n := range nodes {
		if n.ID == nodeID && n.PoolID == poolID {
			return true
		}
	}
	http.Error(w, "token does not match this node's pool", http.StatusForbidden)
	return false
}

// checkServerAdmin answers 401 and returns false unless the bearer token is
// the server admin token. With no token stored yet, nothing matches.
func checkServerAdmin(w http.ResponseWriter, r *http.Request, server store.ServerStore) bool {
	hash, err := server.AdminTokenHash(r.Context())
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return false
	}
	if !checkAdmin(r, hash) {
		http.Error(w, "invalid server admin token", http.StatusUnauthorized)
		return false
	}
	return true
}
