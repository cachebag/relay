package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/cachebag/relay/internal/store"
)

type nodeView struct {
	store.Node
	Status string `json:"status"`
}

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

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		p := store.Pool{
			ID:        store.NewID(),
			Name:      req.Name,
			Token:     store.NewToken(),
			CreatedAt: time.Now().UTC(),
		}

		if err := pools.Create(r.Context(), p); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

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

		for i := range ps {
			ps[i].Token = ""
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ps)
	})

	// Heartbeat endpoint for nodes to report their status.
	mux.HandleFunc("POST /nodes/heartbeat", func(w http.ResponseWriter, r *http.Request) {
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

		p, err := pools.GetByToken(r.Context(), req.PoolToken)

		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid pool token", http.StatusForbidden)
			return
		}

		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		n.PoolID = p.ID
		n.LastSeen = time.Now().UTC()

		if err := reg.Upsert(r.Context(), n); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// List all nodes with online/offline status
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		poolID := r.URL.Query().Get("pool")

		if poolID != "" {
			filtered := make([]store.Node, 0)
			for _, n := range nodes {
				if n.PoolID == poolID {
					filtered = append(filtered, n)
				}
			}
			nodes = filtered
		}

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

	// Creates a new job.
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command []string `json:"command"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		if len(req.Command) == 0 {
			http.Error(w, "command is required", http.StatusBadRequest)
			return
		}

		j := store.Job{
			ID:        store.NewID(),
			Command:   req.Command,
			Status:    store.JobQueued,
			CreatedAt: time.Now().UTC(),
		}

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

	// Lists all jobs.
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, r *http.Request) {
		jobsList, err := jobs.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jobsList)
	})

	// Submit a job result - reject stale results with 409
	mux.HandleFunc("POST /jobs/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		var req struct {
			NodeID  string `json:"node_id"`
			Attempt int    `json:"attempt"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		j, err := jobs.Get(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}

		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		if (j.Status != store.JobAssigned && j.Status != store.JobRunning) ||
			j.NodeID != req.NodeID || j.Attempts != req.Attempt {
			log.Printf("stale result for job %s from %s (attempt %d, current %d)", j.ID, req.NodeID, req.Attempt, j.Attempts)
			http.Error(w, "stale result: job was reassigned", http.StatusConflict)
			return
		}

		j.Status = store.JobDone
		if err := jobs.Update(r.Context(), j); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}