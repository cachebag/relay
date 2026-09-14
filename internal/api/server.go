package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// NewHandler creates and returns the HTTP handler for the relay API.
func NewHandler(reg store.Registry) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	//heartbeat endpoint for nodes to report their status
	mux.HandleFunc("POST /nodes/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var n store.Node

		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		n.LastSeen = time.Now().UTC()

		if err := reg.Upsert(r.Context(), n); err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	//list all nodes endpoint for clients to retrieve the list of registered nodes
	mux.HandleFunc("GET /nodes", func(w http.ResponseWriter, r *http.Request) {
		nodes, err := reg.List(r.Context())
		if err != nil {
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nodes)
	})

	return mux
}
