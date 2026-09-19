package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Sends one heartbeat to the scheduler to inform
// about node status and node system information.
// *http.Client argument is what makes this testable.
func Send(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	n store.Node) error {

	// Converts the node into JSON format.
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}

	// Creates a HTTP POST request and informs the server that
	// the request contains data that is in a JSON format.
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		schedulerURL+"/nodes/heartbeat",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Sends out the HTTP request.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// Closes response body once function has completed running.
	// This line is crucial, as the TCP connection
	// cannot be reused without it.
	defer resp.Body.Close()

	// Sends back response status code. (HTTP 204 is expected)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}
	return nil
}

// Heartbeat that collects the node's system information, then sends
// a heartbeat that includes that system information to
// the scheduler every 'interval'. Since the scheduler being unreachable
// is a normal condition, errors codes returned by the 'Send' function
// are logged and the loop continues. The HTTP Client's 'Timeout'
// will stop the loop after 5 seconds of having a hung connection.
func Run(
	ctx context.Context,
	schedulerURL string,
	interval time.Duration) error {

	// Collection of node's system information
	n, err := Collect()
	if err != nil {
		return err
	}

	// Creation of HTTP Client and Ticker
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(interval)

	// Stops ticker when current function exits,
	// prevents unnecessary resource consumption.
	defer ticker.Stop()

	// Heartbeat loop
	for {
		if err := Send(ctx, client, schedulerURL, n); err != nil {
			log.Printf("heartbeat failed: %v", err)
		} else {
			log.Printf("heartbeat sent to %s", schedulerURL)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
