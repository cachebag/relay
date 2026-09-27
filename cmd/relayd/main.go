package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
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

// Verifies that the specified port is within the valid range (0-65535).
func verifyPort(w io.Writer, port int) {
	// Throws error and exits if port number is invalid
	if port < 0 || port > 65535 {
		logger := log.New(w, "", 0)
		logger.SetOutput(os.Stderr)
		logger.Printf("Invalid port %d, port must be between 0 and 65535\n", port)
		os.Exit(2)
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

	verifyPort(os.Stdout, *port)

	// Starts listening on the specified port.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("relayd listening on :%d", ln.Addr().(*net.TCPAddr).Port)

	// Saves any error as a variable called err, and if an error occurs
	// it will print the error and stop the program
	if err := http.Serve(ln, api.NewHandler(reg, pools, jobs)); err != nil {
		log.Fatal(err)
	}
}
