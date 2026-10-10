package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/cachebag/relay/internal/store"
)

// ErrBadToken is returned when the scheduler rejects the pool token.
// This allows callers to distinguish an invalid token from other
// heartbeat errors, such as connection failures.
var ErrBadToken = errors.New("pool token rejected")

// heartbeat contains the node information and pool token
// that are sent to the scheduler with each heartbeat.
type heartbeat struct {
	store.Node
	PoolToken string `json:"pool_token"`
}

// Send sends one heartbeat to the scheduler to inform it
// about node status and node system information.
// The *http.Client argument makes this function testable.
func Send(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	n store.Node,
	token string,
) error {

	// Converts the heartbeat into JSON format.
	body, err := json.Marshal(heartbeat{
		Node:      n,
		PoolToken: token,
	})
	if err != nil {
		return err
	}

	// Creates an HTTP POST request and informs the server that
	// the request contains data that is in JSON format.
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		schedulerURL+"/nodes/heartbeat",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}

	// Tells the scheduler that the request body contains JSON.
	req.Header.Set("Content-Type", "application/json")

	// Sends out the HTTP request.
	resp, err := client.Do(req)
	if err != nil {
		// Connection errors are returned normally so the caller can
		// distinguish them from an invalid pool token.
		return err
	}

	// Closes the response body once the function has completed running.
	// This line is crucial, as the TCP connection cannot be reused
	// without closing the response body.
	defer resp.Body.Close()

	// A 403 Forbidden response means the scheduler rejected
	// the pool token provided by the agent.
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf(
			"%w: scheduler returned %d",
			ErrBadToken,
			resp.StatusCode,
		)
	}

	// HTTP 204 No Content is the expected successful response.
	// Any other response status is returned as a general scheduler error.
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}

	// The heartbeat was successfully accepted by the scheduler.
	return nil
}
