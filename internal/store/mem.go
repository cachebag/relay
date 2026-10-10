package store

import (
	"context"
	"sort"
	"sync"
)

// Mem is an in-process Registry backed by a map. It's the default for tests and
// for running the control plane without a database; state dies with the process.
type Mem struct {
	// RWMutex rather than Mutex because List is expected to be called far more
	// often than Upsert (heartbeats aside), and concurrent readers shouldn't
	// block each other.
	mu    sync.RWMutex
	nodes map[string]Node
}

// NewMem returns an empty Mem. Use this instead of &Mem{} — the zero value has
// a nil map, which panics on the first Upsert.
func NewMem() *Mem {
	return &Mem{nodes: make(map[string]Node)}
}

// Upsert stores n keyed by its ID, replacing any existing record wholesale. The
// context is unused: there's no I/O to cancel.
func (m *Mem) Upsert(_ context.Context, n Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[n.ID] = n
	return nil
}

// List returns a snapshot copy of every node. Callers get their own slice, so
// they can hold or mutate it without racing a concurrent Upsert.
func (m *Mem) List(_ context.Context) ([]Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, n)
	}
	// Go randomizes map iteration order, so sort before returning to satisfy the
	// Registry contract's stable-ordering guarantee.
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

// Delete removes the node with the given ID, or returns ErrNotFound.
func (m *Mem) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[id]; !ok {
		return ErrNotFound
	}
	delete(m.nodes, id)
	return nil
}
