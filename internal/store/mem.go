package store

import (
	"context"
	"sort"
	"sync"
)

type Mem struct {
	mu    sync.RWMutex
	nodes map[string]Node
}

func NewMem() *Mem {
	return &Mem{nodes: make(map[string]Node)}
}

func (m *Mem) Upsert(_ context.Context, n Node) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[n.ID] = n
	return nil
}

func (m *Mem) List(_ context.Context) ([]Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}
