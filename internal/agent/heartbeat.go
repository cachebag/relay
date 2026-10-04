package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cachebag/relay/internal/store"
)

type heartbeat struct {
	store.Node
	PoolToken string `json:"pool_token"`
}

// Sends one heartbeat to the scheduler to inform
// about node status and node system information.
// *http.Client argument is what makes this testable.
func Send(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	n store.Node,
	token string) error {

	// Converts the heartbeat into JSON format.
	body, err := json.Marshal(heartbeat{Node: n, PoolToken: token})
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
