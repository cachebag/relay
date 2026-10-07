package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ErrNotInPool means the scheduler refused to remove the node because the
// token doesn't belong to the node's pool.
var ErrNotInPool = errors.New("pool token doesn't match this machine's pool")

// Leave asks the scheduler to remove the node with the given ID from its
// pool. A node the scheduler doesn't know counts as success, since it is
// already gone.
func Leave(
	ctx context.Context,
	client *http.Client,
	schedulerURL string,
	nodeID string,
	token string) error {

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		schedulerURL+"/nodes/"+url.PathEscape(nodeID),
		nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotFound:
		return nil
	case http.StatusForbidden:
		return ErrNotInPool
	default:
		return fmt.Errorf("scheduler returned %d", resp.StatusCode)
	}
}
