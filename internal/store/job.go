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
	ID      string    `json:"id"`
	Command []string  `json:"command"`
	Status  JobStatus `json:"status"`
	NodeID  string    `json:"node_id"`
	// Attempts counts how many times the scheduler has placed this job on a
	// node. It is bumped on every assignment, including reassignments after a
	// node goes offline, and the scheduler gives up once it reaches its cap.
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`

	// ExitCode, Stdout and Stderr hold the result a node reports when it
	// finishes the job. They are only served by GET /jobs/{id}/output: stdout
	// and stderr can be large and the dashboard polls the job list, so none of
	// the three is serialized with the job itself.
	//
	// A failed job with ExitCode 0 never reported a result (a reported failure
	// always has a non-zero exit code), which is what the scheduler leaves
	// behind when it gives up on a job.
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
