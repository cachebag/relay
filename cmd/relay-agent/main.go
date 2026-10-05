package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cachebag/relay/internal/agent"
)

func main() {
	// Use RELAY_SCHEDULER as the default scheduler URL when it is set.
	schedulerDefault := os.Getenv("RELAY_SCHEDULER")
	if schedulerDefault == "" {
		schedulerDefault = "http://localhost:8080"
	}

	// Pointer to command-line 'scheduler' flag
	scheduler := flag.String(
		"scheduler",
		schedulerDefault,
		"scheduler base URL")

	// Pointer to command-line 'pool-token' flag
	poolToken := flag.String(
		"pool-token",
		"", "join token for the pool (required)")

	// Makes the 'flag' package inspect command-line argument
	flag.Parse()

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
