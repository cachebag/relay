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

const (
	DefaultJobTimeoutSeconds = 60 * 60
	MaxJobTimeoutSeconds     = 24 * 60 * 60
)

// Stores a job's required operating system.
type Requires struct {
	OS string `json:"os,omitempty"`
}

// Confirms node meets relay operating system requirements.
func (r Requires) Valid() bool {
	switch r.OS {
	case "", "linux", "darwin", "windows":
		return true
	}
	return false
}

// OutputFile is one file a job wrote to out/, as listed from the output
// bundle when it was uploaded.
type OutputFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type Job struct {
	ID string `json:"id"`
	// Command is the command line to run. With a bundle it holds only the
	// arguments passed to Entry, and may be empty.
	Command []string `json:"command"`
	// Entry is the file inside the bundle to run, such as run.sh or
	// crunch.py. Empty with a bundle means the agent picks run.sh, run.ps1 or
	// run.cmd for its OS. Always empty without a bundle.
	Entry string `json:"entry,omitempty"`
	// BundleBytes is the size of the input bundle (a gzipped tar of the
	// submitter's folder) stored beside the job, or 0 if it has none.
	// OutputBytes and OutputFiles describe the out/ bundle the node sent back
	// with the result, once there is one.
	BundleBytes int64        `json:"bundle_bytes"`
	OutputBytes int64        `json:"output_bytes"`
	OutputFiles []OutputFile `json:"output_files,omitempty"`
	// TimeoutSeconds limits each execution attempt. POST /jobs defaults it
	// to one hour and accepts values from 1 second to 24 hours.
	TimeoutSeconds int       `json:"timeout_seconds"`
	Requires       Requires  `json:"requires,omitempty"`
	Status         JobStatus `json:"status"`
	NodeID         string    `json:"node_id"`
	PoolID         string    `json:"pool_id"`
	Submitter      string    `json:"submitter"`
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
