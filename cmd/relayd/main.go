package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/cachebag/relay/internal/api"
	"github.com/cachebag/relay/internal/store"
)

// Package level variable that stores relayd's version number.
var Version = "0.0.0"

// Prints relayd's version number
func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relayd", Version)
}

// Verifies that the specified port is within the valid range (1-65535).
func verifyPort(w io.Writer, port *int) {
	if *port <= 0 || *port > 65535 {
		fmt.Fprintf(w, "Invalid port: %d, must be between 1 and 65535\n", *port)
	}
}

func main() {
	// Creation of version flag
	version := flag.Bool(
		"version",
		false,
		"Prints what version of relayd is being used")

	// Creation of port flag
	port := flag.Int(
		"port",
		8080,
		"Port for relayd to listen on",
	)
	// Makes the 'flag' package inspect cli arguments.
	flag.Parse()

	// Tells the printVersion function to print relayd's version number
	// if the --version flag is passed while the program is running.
	if *version {
		printVersion(os.Stdout)
		return
	}

	reg, err := store.NewSQLite("relay.db")
	if err != nil {
		log.Fatal(err)
	}

	// Creates an in-memory store for pools.
	pools := store.NewMemPools()

	// Creates an in-memory store for jobs.
	jobs := store.NewMemJobs()

	verifyPort(os.Stdout, port)
	log.Printf("relayd listening on :%d", *port)

	// Saves any error as a variable called err, and if an error occurs
	// it will print the error and stop the program
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), api.NewHandler(reg, pools, jobs)); err != nil {
		log.Fatal(err)
	}
}
