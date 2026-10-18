package store

import "context"

// AddTombstone records that t.NodeID was removed from t.PoolID. Adding one
// that already exists just updates its time.
func (s *SQLite) AddTombstone(ctx context.Context, t Tombstone) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO tombstones (pool_id, node_id, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(pool_id, node_id) DO UPDATE SET
		   created_at=excluded.created_at`,
		t.PoolID, t.NodeID, t.CreatedAt.Unix(),
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