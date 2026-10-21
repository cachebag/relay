package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cachebag/relay/internal/store"
)

// pendingFile is the name of the file, inside the agent's cache directory,
// that holds a finished result the scheduler hasn't acknowledged yet.
const pendingFile = "pending.json"

// nodeIDFile stores the agent's stable identity in its cache directory.
const nodeIDFile = "node-id"

// pending is a finished job's result that hasn't been reported yet. The agent
// keeps at most one, and won't ask for new work until it is reported or
// given up on.
type pending struct {
	JobID   string `json:"job_id"`
	Attempt int    `json:"attempt"`
	Result  Result `json:"result"`
}

// defaultCacheDir is where the agent keeps its node ID and pending result:
// relay/ inside the OS's per-user cache directory.
func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		log.Printf("no user cache directory, unreported results won't survive a restart: %v", err)
		return ""
	}
	return filepath.Join(dir, "relay")
}

// Retrieves the agent's stable node ID from the cache directory,
// or creates a new one if it doesn't exist. If a candidate ID is provided, it will
// be used as the node ID if no existing ID is found. The ID is persisted to the
// node-id file in the cache directory. If the cache directory is unavailable, a
// new ID is generated for each run.
func loadOrCreateNodeID(cacheDir, candidate string) string {
	if cacheDir == "" {
		log.Printf("warning: no cache directory, this agent will use a new ID on each run")
		return store.NewID()
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		log.Printf("warning: using a new node ID because cache directory %s is unavailable: %v", cacheDir, err)
		return store.NewID()
	}

	path := filepath.Join(cacheDir, nodeIDFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		log.Printf("warning: using a new node ID because %s could not be read: %v", path, err)
		return store.NewID()
	}

	id := candidate
	if id == "" {
		id = store.NewID()
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		log.Printf("warning: using a new node ID because %s could not be saved: %v", path, err)
		return store.NewID()
	}
	return id
}

// loadPending reads a pending result left behind by a previous run. A file
// that can't be parsed is deleted, since it would never become readable.
func (a *Agent) loadPending() {
	if a.cacheDir == "" {
		return
	}
	path := filepath.Join(a.cacheDir, pendingFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("reading unreported result: %v", err)
		return
	}

	var p pending
	if err := json.Unmarshal(data, &p); err != nil || p.JobID == "" {
		log.Printf("discarding unreadable unreported result in %s", path)
		os.Remove(path)
		return
	}
	log.Printf("found unreported result for job %s from a previous run", p.JobID)
	a.pending = &p
}

// setPending holds on to a finished result and writes it to disk, so a crash
// or restart before it is reported doesn't lose it.
func (a *Agent) setPending(p pending) {
	a.pending = &p
	if a.cacheDir == "" {
		return
	}
	data, err := json.Marshal(p)
	if err == nil {
		err = os.MkdirAll(a.cacheDir, 0o700)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(a.cacheDir, pendingFile), data, 0o600)
	}
	if err != nil {
		log.Printf("saving result for job %s: %v (it won't survive a restart)", p.JobID, err)
	}
}

// clearPending forgets the pending result and deletes its file.
func (a *Agent) clearPending() {
	a.pending = nil
	if a.cacheDir == "" {
		return
	}
	if err := os.Remove(filepath.Join(a.cacheDir, pendingFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("removing unreported result file: %v", err)
	}
}

// flushPending tries to report the pending result, if there is one. It
// returns true when the agent is free to take new work: the result was
// accepted, or the scheduler said it can never be accepted. On a network
// error or 5xx the result is kept for the next interval and it returns false.
func (a *Agent) flushPending(ctx context.Context) bool {
	p := a.pending
	if p == nil {
		return true
	}

	err := a.Report(ctx, store.Job{ID: p.JobID}, resultBody{
		NodeID:  a.node.ID,
		Attempt: p.Attempt,
		Result:  p.Result,
	})
	switch {
	case err == nil:
		log.Printf("job %s finished with exit code %d", p.JobID, p.Result.ExitCode)
	case errors.Is(err, errStale):
		log.Printf("result for job %s discarded: reassigned to another node", p.JobID)
	case errors.Is(err, errRejected):
		log.Printf("result for job %s discarded: %v", p.JobID, err)
	case errors.Is(err, ErrBadToken):
		log.Printf("pool token rejected; check it with your pool admin")
		return false
	default:
		log.Printf("reporting job %s failed, will retry: %v", p.JobID, err)
		return false
	}
	a.clearPending()
	return true
}
