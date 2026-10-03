package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type SQLitePools struct {
	db *sql.DB
}

func NewSQLitePools(db *sql.DB) (*SQLitePools, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS pools (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		token      TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		return nil, err
	}

	return &SQLitePools{db: db}, nil
}

func (s *SQLitePools) Create(ctx context.Context, p Pool) error {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO pools (id, name, token, created_at)
		 VALUES (?, ?, ?, ?)`,
		p.ID,
		p.Name,
		p.Token,
		p.CreatedAt.Unix(),
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	// No row was inserted because a pool with this ID or token already exists.
	if rowsAffected == 0 {
		return ErrExists
	}

	return nil
}

func (s *SQLitePools) Get(ctx context.Context, id string) (Pool, error) {
	var p Pool
	var createdAt int64

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, token, created_at
		 FROM pools
		 WHERE id = ?`,
		id,
	).Scan(
		&p.ID,
		&p.Name,
		&p.Token,
		&createdAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Pool{}, ErrNotFound
	}
	if err != nil {
		return Pool{}, err
	}

	p.CreatedAt = time.Unix(createdAt, 0).UTC()

	return p, nil
}

func (s *SQLitePools) GetByToken(ctx context.Context, token string) (Pool, error) {
	var p Pool
	var createdAt int64

	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, token, created_at
		 FROM pools
		 WHERE token = ?`,
		token,
	).Scan(
		&p.ID,
		&p.Name,
		&p.Token,
		&createdAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Pool{}, ErrNotFound
	}
	if err != nil {
		return Pool{}, err
	}

	p.CreatedAt = time.Unix(createdAt, 0).UTC()

	return p, nil
}

func (s *SQLitePools) List(ctx context.Context) ([]Pool, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, name, token, created_at
		 FROM pools
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pools := make([]Pool, 0)

	for rows.Next() {
		var p Pool
		var createdAt int64

		if err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Token,
			&createdAt,
		); err != nil {
			return nil, err
		}

		p.CreatedAt = time.Unix(createdAt, 0).UTC()
		pools = append(pools, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pools, nil
}
