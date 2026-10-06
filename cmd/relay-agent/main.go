package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cachebag/relay/internal/agent"
)

// Package level variable that stores relay-agent's version number.
var Version = "0.0.0"

// Prints relay-agent's version number
func printVersion(w io.Writer) {
	logger := log.New(w, "", 0)
	logger.Println("relay-agent", Version)
}

// Returns RELAY_SCHEDULER if set, otherwise uses the default scheduler URL.
func schedulerDefault() string {
	if scheduler := os.Getenv("RELAY_SCHEDULER"); scheduler != "" {
		return scheduler
	}
	return "http://localhost:8080"
}

func main() {
	// Creation of version flag
	version := flag.Bool(
		"version",
		false,
		"Prints what version of relay-agent is being used")

	// Pointer to command-line 'scheduler' flag
	scheduler := flag.String(
		"scheduler",
		schedulerDefault(),
		"scheduler base URL")

	// Pointer to command-line 'pool-token' flag
	poolToken := flag.String(
		"pool-token",
		"", "join token for the pool (required)")

	// Makes the 'flag' package inspect command-line argument
	flag.Parse()

	// Prints the version and exits if the --version flag is passed.
	if *version {
		printVersion(os.Stdout)
		return
	}

	// Throws error and exits if pool token is not provided
	if *poolToken == "" {
		fmt.Fprintln(os.Stderr, "relay-agent: --pool-token is required")
		os.Exit(2)
	}

	node, err := agent.Collect()
	if err != nil {
		log.Fatal(err)
	}

	// Cancels the context on Ctrl-C or SIGTERM, which kills a running job
	// and lets Run wait for it before the agent exits.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Every 10 seconds, sends a heartbeat to the scheduler, then fetches,
	// runs and reports the node's next job.
	a := agent.New(*scheduler, *poolToken, node)
	if err := a.Run(ctx, 10*time.Second); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
