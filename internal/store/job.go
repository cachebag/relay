package store

import (
	"context"
	"time"
)

type JobStatus string

const (
	JobQueued   JobStatus = "queued"
	JobAssigned JobStatus = "assigned"
	JobRunning  JobStatus = "running"
	JobDone     JobStatus = "done"
	JobFailed   JobStatus = "failed"
)

type Job struct {
	ID        string    `json:"id"`
	Command   []string  `json:"command"`
	Status    JobStatus `json:"status"`
	NodeID    string    `json:"node_id"`
	CreatedAt time.Time `json:"created_at"`
}

type JobStore interface {
	Create(ctx context.Context, j Job) error
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context) ([]Job, error)
	Update(ctx context.Context, j Job) error
}
