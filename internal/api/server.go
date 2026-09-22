package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// NewHandler creates and returns the HTTP handler for the relay API.
func NewHandler(reg store.Registry, pools store.PoolStore, jobs store.JobStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Creates a new pool.
	mux.HandleFunc("POST /pools", func(w http.ResponseWriter, r *http.Request) {
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

		// Return the created pool.
		// This is the only endpoint that returns the pool token.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(p)
	})

	// Lists all pools.
	mux.HandleFunc("GET /pools", func(w http.ResponseWriter, r *http.Request) {
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

		if err := reg.Upsert(r.Context(), n); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// List all nodes endpoint for clients to retrieve registered nodes.
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		// Check whether a specific pool was requested.
		poolID := r.URL.Query().Get("pool")

		if poolID != "" {
			filtered := make([]store.Node, 0)

			// Only keep nodes that belong to the requested pool.
			for _, n := range nodes {
				if n.PoolID == poolID {
					filtered = append(filtered, n)
				}
			}

			nodes = filtered
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nodes)
	})

	// Creates a new job.
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command []string `json:"command"`
		}

		// Read the command from the request.
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if len(req.Command) == 0 {
			http.Error(w, "command is required", http.StatusBadRequest)
			return
		}

		// Create the job with a server-generated ID and queued status.
		j := store.Job{
			ID:        store.NewID(),
			Command:   req.Command,
			Status:    store.JobQueued,
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

	// Gets a job by ID.
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		j, err := jobs.Get(r.Context(), id)

		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}

		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(j)
	})

	return mux
}
