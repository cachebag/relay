package main

import (
	"log"
	"net/http"

	"github.com/cachebag/relay/internal/api"
)

func main() {
	log.Println("relayd listening on :8080")

	if err := http.ListenAndServe(":8080", api.NewHandler()); err != nil {
		log.Fatal(err)
	}
}
