package store

import (
	"context"
	"slices"
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
	j.Command = slices.Clone(j.Command)
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
	j.Command = slices.Clone(j.Command)
	return j, nil
}

func (m *MemJobs) List(_ context.Context) ([]Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		j.Command = slices.Clone(j.Command)
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemJobs) Update(_ context.Context, j Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.Command = slices.Clone(j.Command)
	m.jobs[j.ID] = j
	return nil
}
