package store

import (
	"context"
	"sync"
)

type MemCredits struct {
	mu      sync.RWMutex
	entries []CreditEntry
	ids     map[string]bool
}

func NewMemCredits() *MemCredits {
	return &MemCredits{ids: make(map[string]bool)}
}

func (m *MemCredits) Add(_ context.Context, e CreditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ids[e.ID] {
		return ErrExists
	}
	m.ids[e.ID] = true
	m.entries = append(m.entries, e)
	return nil
}

func (m *MemCredits) Balances(_ context.Context, poolID string) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]int)
	for _, e := range m.entries {
		if e.PoolID == poolID {
			out[e.Account] += e.Delta
		}
	}
	return out, nil
}
