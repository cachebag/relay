package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cachebag/relay/internal/store"
)

// Verifies that the agent correctly persists its node ID across restarts.
func TestAgentPersistsNodeID(t *testing.T) {
	cacheDir := t.TempDir()
	node := store.Node{Hostname: "test-host"}

	first := newAgent("", "", node, cacheDir)
	if first.node.ID == "" {
		t.Fatal("first agent has an empty ID")
	}
	data, err := os.ReadFile(filepath.Join(cacheDir, nodeIDFile))
	if err != nil {
		t.Fatalf("reading node ID file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != first.node.ID {
		t.Fatalf("node-id contains %q, want %q", got, first.node.ID)
	}

	second := newAgent("", "", node, cacheDir)
	if second.node.ID != first.node.ID {
		t.Errorf("second agent ID = %q, want persisted ID %q", second.node.ID, first.node.ID)
	}
}

// Verifies that the agent generates a fresh node ID when the cache is not usable.
func TestNodeIDHandlingNoUsableCache(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(cachePath, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := newAgent("", "", store.Node{Hostname: "test-host"}, cachePath)
	if len(a.node.ID) != 12 {
		t.Errorf("agent ID = %q, want a generated 12-character ID", a.node.ID)
	}
}

// Agents for different pools share a machine identity but never share job
// folders or pending results, even when one restarts and cleans up old jobs.
func TestPoolCachesKeepJobsAndResultsSeparate(t *testing.T) {
	root := t.TempDir()
	identityDir := filepath.Join(root, "relay")
	poolA := filepath.Join(identityDir, "agents", "pool-a")
	poolB := filepath.Join(identityDir, "agents", "pool-b")
	node := store.Node{Hostname: "test-host"}

	a := newAgentWithDirs("", "token-a", node, poolA, identityDir)
	b := newAgentWithDirs("", "token-b", node, poolB, identityDir)
	if a.node.ID == "" || a.node.ID != b.node.ID {
		t.Fatalf("node IDs differ: %q and %q", a.node.ID, b.node.ID)
	}
	if _, err := os.Stat(filepath.Join(identityDir, nodeIDFile)); err != nil {
		t.Fatalf("shared node ID was not persisted: %v", err)
	}

	a.setPending(pending{JobID: "job-a", Attempt: 1})
	b.setPending(pending{JobID: "job-b", Attempt: 1})
	for _, dir := range []string{poolA, poolB} {
		if _, err := os.Stat(filepath.Join(dir, pendingFile)); err != nil {
			t.Errorf("missing pending result in %s: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(identityDir, pendingFile)); !os.IsNotExist(err) {
		t.Errorf("pending result was written to shared identity directory: %v", err)
	}

	if _, err := a.makeWorkdir("job-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.makeWorkdir("job-b"); err != nil {
		t.Fatal(err)
	}
	b.cleanWorkdirs()
	if _, err := os.Stat(filepath.Join(poolA, jobsDir, "job-a")); err != nil {
		t.Fatalf("pool B cleanup touched pool A's job: %v", err)
	}

	restartedA := newAgentWithDirs("", "token-a", node, poolA, identityDir)
	restartedB := newAgentWithDirs("", "token-b", node, poolB, identityDir)
	if restartedA.pending == nil || restartedA.pending.JobID != "job-a" {
		t.Errorf("pool A's pending result was not recovered")
	}
	if restartedB.pending == nil || restartedB.pending.JobID != "job-b" {
		t.Errorf("pool B's pending result was not recovered")
	}
}
