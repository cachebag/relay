package store

import (
	"context"
	"time"
)

// JobStatus tracks a job through its lifecycle: queued on submission, assigned
// once the scheduler picks a node, running once that node starts it, then done
// or failed. Only these five values are ever stored.
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
	PoolID    string    `json:"pool_id"`
	Submitter string    `json:"submitter"`
	// Attempts counts how many times the scheduler has placed this job on a
	// node. It is bumped on every assignment, including reassignments after a
	// node goes offline, and the scheduler gives up once it reaches its cap.
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
	// StartedAt is set when a node picks the job up and cleared if the job is
	// requeued, so it always covers the current attempt. FinishedAt is set
	// when a result is accepted or the scheduler gives up. Both stay out of
	// the JSON until set.
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// ExitCode, Stdout and Stderr hold the result a node reported. A reported
	// failure always has a non-zero ExitCode, so a failed job with ExitCode 0
	// never reported (the scheduler gave up on it). Stdout and Stderr stay out
	// of the JSON because the dashboard polls the job list every 2 seconds;
	// GET /jobs/{id}/output serves them instead.
	ExitCode int    `json:"-"`
	Stdout   string `json:"-"`
	Stderr   string `json:"-"`
}

// JobStore is the storage contract for jobs, mirroring Registry for nodes:
// MemJobs here, SQLite later, and callers never depend on which one they got.
//
// Implementations must hold to what the shared test in jobstore_test.go checks:
//   - Create returns ErrExists if the ID is already taken.
//   - Get and Update return ErrNotFound for an unknown ID.
//   - List returns a stable ordering, oldest first.
//   - Stored jobs are independent of the caller's Job value, so mutating a
//     Command slice after the call can't reach into the store.
type JobStore interface {
	Create(ctx context.Context, j Job) error
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context) ([]Job, error)
	Update(ctx context.Context, j Job) error
}
