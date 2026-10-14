package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/cachebag/relay/internal/api"
	"github.com/cachebag/relay/internal/credit"
	"github.com/cachebag/relay/internal/scheduler"
	"github.com/cachebag/relay/internal/store"
)

// Package level variable that stores relayd's version number.
var Version = "0.0.0"

// Prints relayd's version number
func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relayd", Version)
}

// Returns an error if the port is outside 0-65535. 0 lets the OS pick a free port.
func validPort(p int) error {
	if p < 0 || p > 65535 {
		return fmt.Errorf("invalid port %d, must be between 0 and 65535", p)
	}
	return nil
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

	if err := validPort(*port); err != nil {
		fmt.Fprintln(os.Stderr, "relayd:", err)
		os.Exit(2)
	}

	// Tells the printVersion function to print relayd's version number
	// if the --version flag is passed while the program is running.
	if *version {
		printVersion(os.Stdout)
		return
	}

	// Opens the SQLite database used by nodes, pools, and jobs.
	reg, err := store.NewSQLite("relay.db")
	if err != nil {
		log.Fatal(err)
	}
	defer reg.Close()

	// Creates a SQLite-backed store for pools using the shared database.
	pools, err := store.NewSQLitePools(reg.DB())
	if err != nil {
		log.Fatal(err)
	}

	// Creates a SQLite-backed store for jobs using the shared database.
	jobs, err := store.NewSQLiteJobs(reg.DB())
	if err != nil {
		log.Fatal(err)
	}

	// Creates a SQLite-backed credit ledger using the shared database.
	credits, err := store.NewSQLiteCredits(reg.DB())
	if err != nil {
		log.Fatal(err)
	}

	// Starts listening on the specified port.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("relayd listening on :%d", ln.Addr().(*net.TCPAddr).Port)

	// Starts the scheduler in the background.
	go scheduler.New(reg, jobs, credit.NewEarner(credits)).Run(context.Background(), 2*time.Second)

	// Saves any error as a variable called err, and if an error occurs
	// it will print the error and stop the program.
	if err := http.Serve(ln, api.NewHandler(reg, pools, jobs, credits)); err != nil {
		log.Fatal(err)
	}
}
