package store

import (
	"context"
	"sort"
	"sync"
)

type MemPools struct {
	mu    sync.RWMutex
	pools map[string]Pool
}

func NewMemPools() *MemPools {
	return &MemPools{pools: make(map[string]Pool)}
}

func (m *MemPools) Create(_ context.Context, p Pool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pools[p.ID] = p
	return nil
}

func (m *MemPools) Get(_ context.Context, id string) (Pool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	if !ok {
		return Pool{}, ErrNotFound
	}
	return p, nil
}

func (m *MemPools) GetByToken(_ context.Context, token string) (Pool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.pools {
		if p.Token == token {
			return p, nil
		}
	}
	return Pool{}, ErrNotFound
}

func (m *MemPools) List(_ context.Context) ([]Pool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Pool, 0, len(m.pools))
	for _, p := range m.pools {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemPools) SetToken(_ context.Context, id, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok {
		return ErrNotFound
	}
	p.Token = token
	m.pools[id] = p
	return nil
}

func (m *MemPools) SetAdminTokenHash(_ context.Context, id, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pools[id]
	if !ok {
		return ErrNotFound
	}
	p.AdminTokenHash = hash
	m.pools[id] = p
	return nil
}

func (m *MemPools) AdminTokenHash(_ context.Context, id string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[id]
	if !ok {
		return "", ErrNotFound
	}
	return p.AdminTokenHash, nil
}