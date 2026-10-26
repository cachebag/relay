package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
		strings.TrimRight(schedulerURL, "/")+"/nodes/"+url.PathEscape(nodeID),
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

// ClearLocalData deletes what the agent keeps in its cache directory once
// the machine has left its pool, and returns the paths it removed.
func ClearLocalData() ([]string, error) {
	return clearLocalData(defaultCacheDir())
}

// clearLocalData is ClearLocalData with the directory chosen by the caller,
// so tests can use a temp directory. The directory itself is only removed
// if it is empty afterwards, so nothing the agent didn't create is touched.
func clearLocalData(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}

	var removed []string
	path := filepath.Join(dir, pendingFile)
	if err := os.Remove(path); err == nil {
		removed = append(removed, path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return removed, err
	}

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return removed, nil
	}
	if err != nil {
		return removed, err
	}
	if len(entries) == 0 {
		if err := os.Remove(dir); err != nil {
			return removed, err
		}
		removed = append(removed, dir)
	}
	return removed, nil
}
