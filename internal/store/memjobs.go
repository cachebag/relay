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

// Create refuses an ID that already exists, so a caller that retries a
// submission can tell a duplicate from a fresh job instead of silently
// clobbering the original.
func (m *MemJobs) Create(_ context.Context, j Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[j.ID]; ok {
		return ErrExists
	}
	// Clone so the store owns its copy: Command is a slice, and without this the
	// caller keeps a reference into the stored job and can mutate it from outside.
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
	// Hand back a copy for the same reason Create takes one.
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
	// Map iteration order is randomized, so List must sort to keep the stable
	// ordering callers expect. CreatedAt alone isn't enough: two jobs created in
	// the same clock tick would still shuffle between calls, so ID breaks ties.
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// Update refuses an ID that isn't there rather than creating it, so a stale
// caller updating a job that no longer exists gets an error instead of
// resurrecting a half-filled record.
func (m *MemJobs) Update(_ context.Context, j Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[j.ID]; !ok {
		return ErrNotFound
	}
	j.Command = slices.Clone(j.Command)
	m.jobs[j.ID] = j
	return nil
}
