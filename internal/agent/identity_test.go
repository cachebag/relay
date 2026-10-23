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
