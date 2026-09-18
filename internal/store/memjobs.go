package store

import (
	"context"
	"sort"
	"sync"
)

type MemJobs struct {
	mu   sync.RWMutex
	jobs map[string]Job
}

func NewMemJobs() *MemJobs {
	return &MemJobs{jobs: make(map[string]Job)}
}

func (m *MemJobs) Create(_ context.Context, j Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[j.ID] = j
	return nil
}

func (m *MemJobs) Get(_ context.Context, id string) (Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return j, nil
}

func (m *MemJobs) List(_ context.Context) ([]Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemJobs) Update(_ context.Context, j Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[j.ID] = j
	return nil
}
