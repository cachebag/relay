package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SQLiteJobs stores jobs in a SQLite database.
type SQLiteJobs struct {
	db *sql.DB
}

// NewSQLiteJobs creates a SQLite-backed job store using the shared database
// connection. The jobs table is created automatically if it does not exist.
func NewSQLiteJobs(db *sql.DB) (*SQLiteJobs, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS jobs (
		id              TEXT PRIMARY KEY,
		command         TEXT NOT NULL,
		timeout_seconds INTEGER NOT NULL DEFAULT 3600,
		requires        TEXT NOT NULL DEFAULT '{}',
		status          TEXT NOT NULL,
		node_id         TEXT NOT NULL,
		pool_id         TEXT NOT NULL DEFAULT '',
		submitter       TEXT NOT NULL DEFAULT '',
		attempts        INTEGER NOT NULL,
		created_at      INTEGER NOT NULL,
		exit_code       INTEGER NOT NULL DEFAULT 0,
		stdout          TEXT NOT NULL DEFAULT '',
		stderr          TEXT NOT NULL DEFAULT '',
		started_at      INTEGER NOT NULL DEFAULT 0,
		finished_at     INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		return nil, err
	}

	// Databases created before results were stored lack these columns. Adding
	// one that already exists fails harmlessly, so the errors are ignored.
	_, _ = db.Exec(`ALTER TABLE jobs ADD COLUMN exit_code INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE jobs ADD COLUMN stdout TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE jobs ADD COLUMN stderr TEXT NOT NULL DEFAULT ''`)

	// Add job ownership columns to databases created before I79 was made.
	// Check for missing columns explicitly so unexpected migration failures surface.
	for _, column := range []string{"pool_id", "submitter"} {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name = ?`,
			column,
		).Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			if _, err := db.Exec(
				`ALTER TABLE jobs ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`,
			); err != nil {
				return nil, err
			}
		}
	}

	// Job requirements are stored as JSON, same as command.
	var hasRequires int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name = 'requires'`,
	).Scan(&hasRequires); err != nil {
		return nil, err
	}

	// Adds the requires column if it is missing (legacy support).
	if hasRequires == 0 {
		if _, err := db.Exec(
			`ALTER TABLE jobs ADD COLUMN requires TEXT NOT NULL DEFAULT '{}'`,
		); err != nil {
			return nil, err
		}
	}

	// Existing databases did not store job time limits. Backfill them with
	// the same one-hour default used for new API submissions.
	var hasTimeout int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name = 'timeout_seconds'`,
	).Scan(&hasTimeout); err != nil {
		return nil, err
	}
	if hasTimeout == 0 {
		if _, err := db.Exec(
			`ALTER TABLE jobs ADD COLUMN timeout_seconds INTEGER NOT NULL DEFAULT 3600`,
		); err != nil {
			return nil, err
		}
	}

	// Timestamps for the current attempt and the finish, added with credits
	// charging. 0 means unset.
	for _, column := range []string{"started_at", "finished_at"} {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name = ?`,
			column,
		).Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			if _, err := db.Exec(
				`ALTER TABLE jobs ADD COLUMN ` + column + ` INTEGER NOT NULL DEFAULT 0`,
			); err != nil {
				return nil, err
			}
		}
	}

	return &SQLiteJobs{db: db}, nil
}

// Returns a Requires struct and any error encountered during unmarshalling.
// Treats empty strings as no requirements.
func convertRequires(s string) (Requires, error) {
	var r Requires
	if s == "" {
		return r, nil
	}
	err := json.Unmarshal([]byte(s), &r)
	return r, err
}

// unixOrZero stores an unset timestamp as 0.
func unixOrZero(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}

// timeOrNil reads a 0 back as unset.
func timeOrNil(sec int64) *time.Time {
	if sec == 0 {
		return nil
	}
	t := time.Unix(sec, 0).UTC()
	return &t
}

// Create inserts a new job into SQLite.
//
// Command is stored as JSON because Job.Command is a []string and SQLite does
// not have a native string-slice type.
//
// INSERT OR IGNORE lets us detect a duplicate job ID and return ErrExists,
// matching the JobStore contract.
func (s *SQLiteJobs) Create(ctx context.Context, j Job) error {
	command, err := json.Marshal(j.Command)
	if err != nil {
		return err
	}

	requires, err := json.Marshal(j.Requires)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO jobs (
			id,
			command,
			timeout_seconds,
			requires,
			status,
			node_id,
			pool_id,
			submitter,
			attempts,
			created_at,
			exit_code,
			stdout,
			stderr,
			started_at,
			finished_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID,
		string(command),
		j.TimeoutSeconds,
		string(requires),
		j.Status,
		j.NodeID,
		j.PoolID,
		j.Submitter,
		j.Attempts,
		j.CreatedAt.Unix(),
		j.ExitCode,
		j.Stdout,
		j.Stderr,
		unixOrZero(j.StartedAt),
		unixOrZero(j.FinishedAt),
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// No row was inserted because a job with this ID already exists.
	if rowsAffected == 0 {
		return ErrExists
	}

	return nil
}

// Get returns a job by ID.
//
// If the ID does not exist, ErrNotFound is returned to match the JobStore
// interface behavior used by the in-memory implementation.
func (s *SQLiteJobs) Get(ctx context.Context, id string) (Job, error) {
	var j Job
	var command, requires string
	var createdAt, startedAt, finishedAt int64

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, command, timeout_seconds, requires, status, node_id, pool_id, submitter, attempts, created_at, exit_code, stdout, stderr, started_at, finished_at
		 FROM jobs
		 WHERE id = ?`,
		id,
	).Scan(
		&j.ID,
		&command,
		&j.TimeoutSeconds,
		&requires,
		&j.Status,
		&j.NodeID,
		&j.PoolID,
		&j.Submitter,
		&j.Attempts,
		&createdAt,
		&j.ExitCode,
		&j.Stdout,
		&j.Stderr,
		&startedAt,
		&finishedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}

	// Convert the JSON command stored in SQLite back into []string.
	if err := json.Unmarshal([]byte(command), &j.Command); err != nil {
		return Job{}, err
	}
	if j.Requires, err = convertRequires(requires); err != nil {
		return Job{}, err
	}

	// Timestamps are stored as Unix seconds in SQLite.
	j.CreatedAt = time.Unix(createdAt, 0).UTC()
	j.StartedAt = timeOrNil(startedAt)
	j.FinishedAt = timeOrNil(finishedAt)

	return j, nil
}

// List returns all jobs ordered from oldest to newest.
//
// ID is used as a secondary sort key so the result remains deterministic when
// two jobs have the same creation timestamp.
func (s *SQLiteJobs) List(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, command, timeout_seconds, requires, status, node_id, pool_id, submitter, attempts, created_at, exit_code, stdout, stderr, started_at, finished_at
		 FROM jobs
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]Job, 0)

	for rows.Next() {
		var j Job
		var command, requires string
		var createdAt, startedAt, finishedAt int64

		if err := rows.Scan(
			&j.ID,
			&command,
			&j.TimeoutSeconds,
			&requires,
			&j.Status,
			&j.NodeID,
			&j.PoolID,
			&j.Submitter,
			&j.Attempts,
			&createdAt,
			&j.ExitCode,
			&j.Stdout,
			&j.Stderr,
			&startedAt,
			&finishedAt,
		); err != nil {
			return nil, err
		}

		// Rebuild the original []string command from its JSON representation.
		if err := json.Unmarshal([]byte(command), &j.Command); err != nil {
			return nil, err
		}
		if j.Requires, err = convertRequires(requires); err != nil {
			return nil, err
		}

		j.CreatedAt = time.Unix(createdAt, 0).UTC()
		j.StartedAt = timeOrNil(startedAt)
		j.FinishedAt = timeOrNil(finishedAt)

		jobs = append(jobs, j)
	}

	// rows.Err catches errors that occur while iterating through the query
	// results rather than during the initial query itself.
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return jobs, nil
}

// Update replaces the stored fields for an existing job.
//
// If no row matches the supplied job ID, ErrNotFound is returned.
func (s *SQLiteJobs) Update(ctx context.Context, j Job) error {
	command, err := json.Marshal(j.Command)
	if err != nil {
		return err
	}
	requires, err := json.Marshal(j.Requires)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs
		 SET command = ?,
		     timeout_seconds = ?,
		     requires = ?,
		     status = ?,
		     node_id = ?,
		     pool_id = ?,
		     submitter = ?,
		     attempts = ?,
		     created_at = ?,
		     exit_code = ?,
		     stdout = ?,
		     stderr = ?,
		     started_at = ?,
		     finished_at = ?
		 WHERE id = ?`,
		string(command),
		j.TimeoutSeconds,
		string(requires),
		j.Status,
		j.NodeID,
		j.PoolID,
		j.Submitter,
		j.Attempts,
		j.CreatedAt.Unix(),
		j.ExitCode,
		j.Stdout,
		j.Stderr,
		unixOrZero(j.StartedAt),
		unixOrZero(j.FinishedAt),
		j.ID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// An update affecting zero rows means the requested job does not exist.
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}
