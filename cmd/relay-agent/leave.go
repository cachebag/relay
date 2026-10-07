package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cachebag/relay/internal/agent"
	"github.com/cachebag/relay/internal/store"
)

// runLeave handles `relay-agent leave`: it takes this machine out of its pool
// and deletes the agent's local data. It returns the exit code.
func runLeave(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("leave", flag.ContinueOnError)
	fs.SetOutput(stderr)

	scheduler := fs.String("scheduler", schedulerDefault(), "scheduler base URL")
	poolToken := fs.String("pool-token", poolTokenDefault(), "join token for the pool (or set RELAY_POOL_TOKEN)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *poolToken == "" {
		fmt.Fprintln(stderr, "relay-agent leave: pool token is required, use --pool-token or set RELAY_POOL_TOKEN")
		return 2
	}

	node, err := agent.Collect()
	if err != nil {
		fmt.Fprintf(stderr, "relay-agent leave: %v\n", err)
		return 1
	}

	client := &http.Client{Timeout: 5 * time.Second}
	return leave(context.Background(), client, *scheduler, *poolToken, node, agent.ClearLocalData, stdout, stderr)
}

// leave removes node from its pool, then clears local data with clear. It is
// separate from runLeave so tests can pass a test server and a fake clear.
func leave(
	ctx context.Context,
	client *http.Client,
	scheduler string,
	token string,
	node store.Node,
	clear func() ([]string, error),
	stdout io.Writer,
	stderr io.Writer) int {

	err := agent.Leave(ctx, client, scheduler, node.ID, token)
	var netErr *url.Error
	switch {
	case errors.Is(err, agent.ErrNotInPool):
		fmt.Fprintln(stderr, "relay-agent leave: the pool token doesn't match this machine's pool")
		return 1
	case errors.As(err, &netErr):
		fmt.Fprintf(stderr, "relay-agent leave: can't reach the scheduler at %s\n", scheduler)
		fmt.Fprintln(stderr, "nothing was changed, try again once the scheduler is reachable")
		return 1
	case err != nil:
		fmt.Fprintf(stderr, "relay-agent leave: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "%s left the pool\n", node.Hostname)

	// A result still waiting to be reported belongs to a job that has just
	// gone back in the queue, so there is nothing worth keeping.
	removed, err := clear()
	for _, path := range removed {
		fmt.Fprintf(stdout, "removed %s\n", path)
	}
	if err != nil {
		fmt.Fprintf(stderr, "relay-agent leave: couldn't remove the agent's local data: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "if relay-agent is still running on this machine, stop it or it will join the pool again")
	return 0
}
