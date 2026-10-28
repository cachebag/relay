package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	_ "io"
	"net/http"
	"strings"
	_ "strings"

	"github.com/cachebag/relay/internal/store"
)

// ErrBadToken is returned when the scheduler rejects the pool token.
// This allows callers to distinguish an invalid token from other
// heartbeat errors, such as connection failures.
var ErrBadToken = errors.New("pool token rejected")
var ErrTokenRotated = errors.New(store.RotatedTokenMsg)

// ErrPoolConflict is returned when the node ID already belongs to another pool.
var ErrPoolConflict = errors.New(
	"node ID already belongs to another pool; leave that pool before joining this one",
)

// ErrRemoved is returned when the scheduler answers 410 Gone, meaning a pool
// admin removed this machine from the pool. Unlike a connection failure,
// retrying will never succeed, so the agent stops.
var ErrRemoved = errors.New("removed from pool by admin")
var ErrTokenRotated = errors.New(store.RotatedTokenMsg)

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

	// A 410 Gone response means a pool admin removed this machine.
	if resp.StatusCode == http.StatusGone {
		return fmt.Errorf(
			"%w: scheduler returned %d",
			ErrRemoved,
			resp.StatusCode,
		)
	}

	// A 403 Forbidden response means the scheduler rejected
	// the pool token provided by the agent.
	if resp.StatusCode == http.StatusForbidden {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if strings.Contains(string(msg), store.RotatedTokenMsg) {
			return fmt.Errorf("%w: %w", ErrBadToken, ErrTokenRotated)
		}
		return fmt.Errorf(
			"%w: scheduler returned %d",
			ErrBadToken,
			resp.StatusCode,
		)
	}

	// A 409 Conflict response means the node ID already belongs to another pool.
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("%w: scheduler returned %d", ErrPoolConflict, resp.StatusCode)
	}

	// HTTP 204 No Content is the expected successful response.
	// Any other response status is returned as a general scheduler error.
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}

	// The heartbeat was successfully accepted by the scheduler.
	return nil
}
