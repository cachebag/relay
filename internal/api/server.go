package api

import "net/http"

// NewHandler creates and returns the HTTP handler for the relay API.
func NewHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return mux
}
