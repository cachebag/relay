package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cachebag/relay/internal/agent"
)

func main() {
	// Pointer to command-line 'scheduler' flag
	scheduler := flag.String(
		"scheduler",
		"http://localhost:8080",
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

	// Repeatedly sends a heartbeat every 10 seconds to scheduler
	if err := agent.Run(
		context.Background(),
		*scheduler,
		10*time.Second); err != nil {
		log.Fatal(err)
	}
}
