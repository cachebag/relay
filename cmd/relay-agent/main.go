package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
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

	// Container image every job runs in.
	image := flag.String(
		"image",
		"alpine:3.20",
		"docker image jobs run in")

	// Docker network mode for job containers.
	network := flag.String(
		"network",
		"none",
		"docker network mode for job containers (none disables networking)")

	// Makes the 'flag' package inspect command-line argument
	flag.Parse()

	// Throws error and exits if pool token is not provided
	if *poolToken == "" {
		fmt.Fprintln(os.Stderr, "relay-agent: --pool-token is required")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Jobs only ever run inside docker, so refuse to start without it.
	runner := agent.NewDockerRunner(*image, *network)
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err := runner.Check(checkCtx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}

	node, err := agent.Collect()
	if err != nil {
		log.Fatal(err)
	}

	// Picks up assigned jobs and runs them in containers.
	go func() {
		if err := agent.RunJobs(ctx, *scheduler, node.ID, runner, 2*time.Second); err != nil && ctx.Err() == nil {
			log.Fatal(err)
		}
	}()

	// Repeatedly sends a heartbeat every 10 seconds to scheduler
	if err := agent.Run(
		ctx,
		*scheduler,
		*poolToken,
		10*time.Second); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
