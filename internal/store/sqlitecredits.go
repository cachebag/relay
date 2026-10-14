package store

import (
	"context"
	"database/sql"
)

// SQLiteCredits stores the credit ledger in a SQLite database.
type SQLiteCredits struct {
	db *sql.DB
}

// NewSQLiteCredits creates a SQLite-backed credit store using the shared
// database connection. The credits table is created automatically if it does
// not exist.
func NewSQLiteCredits(db *sql.DB) (*SQLiteCredits, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS credits (
		id         TEXT PRIMARY KEY,
		pool_id    TEXT NOT NULL,
		account    TEXT NOT NULL,
		delta      INTEGER NOT NULL,
		reason     TEXT NOT NULL,
		job_id     TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		return nil, err
	}

	// Balances always filters by pool, so index it.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS credits_pool_id ON credits (pool_id)`); err != nil {
		return nil, err
	}

	return &SQLiteCredits{db: db}, nil
}

func (s *SQLiteCredits) Add(ctx context.Context, e CreditEntry) error {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO credits (id, pool_id, account, delta, reason, job_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.PoolID, e.Account, e.Delta, e.Reason, e.JobID, e.CreatedAt.Unix(),
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// No row was inserted because an entry with this ID already exists.
	if rowsAffected == 0 {
		return ErrExists
	}
	return nil
}

func (s *SQLiteCredits) Balances(ctx context.Context, poolID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT account, SUM(delta) FROM credits WHERE pool_id = ? GROUP BY account`,
		poolID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var account string
		var balance int
		if err := rows.Scan(&account, &balance); err != nil {
			return nil, err
		}
		out[account] = balance
	}
	return out, rows.Err()
}
