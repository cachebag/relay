package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/cachebag/relay/internal/api"
	"github.com/cachebag/relay/internal/store"
)

var Version = "0.0.0"

func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relayd", Version)
}

func main() {
	version := flag.Bool(
		"version",
		false,
		"Prints what version of relayd is being used")
	flag.Parse()

	if *version {
		printVersion(os.Stdout)
		return
	}

	reg, err := store.NewSQLite("relay.db")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("relayd listening on :8080")

	if err := http.ListenAndServe(":8080", api.NewHandler(reg)); err != nil {
		log.Fatal(err)
	}
}
