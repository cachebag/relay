package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AddTombstone records that t.NodeID was removed from t.PoolID. Adding one
// that already exists updates its time and its token hash, meaning a second
// removal after the machine has already rejoined results in the current
// token being blocked.
func (s *SQLite) AddTombstone(ctx context.Context, t Tombstone) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO tombstones (pool_id, node_id, created_at, token_hash)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(pool_id, node_id) DO UPDATE SET
		   created_at=excluded.created_at,
		   token_hash=excluded.token_hash`,
		t.PoolID, t.NodeID, t.CreatedAt.Unix(), t.TokenHash,
	)
	return err
}

// HasTombstone reports whether nodeID was removed from poolID.
func (s *SQLite) HasTombstone(ctx context.Context, poolID, nodeID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM tombstones WHERE pool_id = ? AND node_id = ?`,
		poolID, nodeID,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetTombstone returns the tombstone for nodeID in poolID.
func (s *SQLite) GetTombstone(ctx context.Context, poolID, nodeID string) (Tombstone, bool, error) {
	var t Tombstone
	var sec int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT pool_id, node_id, created_at, token_hash
		 FROM tombstones WHERE pool_id = ? AND node_id = ?`,
		poolID, nodeID,
	).Scan(&t.PoolID, &t.NodeID, &sec, &t.TokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Tombstone{}, false, nil
	}
	if err != nil {
		return Tombstone{}, false, err
	}
	t.CreatedAt = time.Unix(sec, 0).UTC()
	return t, true, nil
}

// DeleteTombstone removes the tombstone for nodeID in poolID.
func (s *SQLite) DeleteTombstone(ctx context.Context, poolID, nodeID string) error {
	_, err := s.db.ExecContext(
		ctx,
		`DELETE FROM tombstones WHERE pool_id = ? AND node_id = ?`,
		poolID, nodeID,
	)
	return err
}

// migrateTombstoneTokenHash adds token_hash to relay.db files created before
// a removal recorded which join token is blocked. Empty hashes are filled
// from the pool's current token. Hashes that are already set are left alone.
func migrateTombstoneTokenHash(db *sql.DB) error {
	var has int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('tombstones') WHERE name = 'token_hash'`,
	).Scan(&has); err != nil {
		return err
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE tombstones ADD COLUMN token_hash TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	var pools int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pools'`,
	).Scan(&pools); err != nil {
		return err
	}
	if pools == 0 {
		return nil
	}

	rows, err := db.Query(
		`SELECT t.pool_id, t.node_id, p.token
		 FROM tombstones t
		 JOIN pools p ON p.id = t.pool_id
		 WHERE t.token_hash = '' AND p.token != ''`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type fill struct {
		poolID string
		nodeID string
		token  string
	}
	var pending []fill
	for rows.Next() {
		var f fill
		if err := rows.Scan(&f.poolID, &f.nodeID, &f.token); err != nil {
			return err
		}
		pending = append(pending, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if err := rows.Close(); err != nil {
		return err
	}

	for _, f := range pending {
		if _, err := db.Exec(
			`UPDATE tombstones SET token_hash = ? WHERE pool_id = ? AND node_id = ? AND token_hash = ''`,
			HashToken(f.token), f.poolID, f.nodeID,
		); err != nil {
			return err
		}
	}
	return nil
}
