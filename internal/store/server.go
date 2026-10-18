package store

import (
	"context"
	"database/sql"
	"sync"
)

// ServerStore keeps the one secret that belongs to the scheduler itself
// rather than to a pool: the hash of the server admin token. Only its
// holder can create or list pools, so once relayd is on the internet
// strangers can't make pools on it. The token is never stored, only its
// hash, the same as pool admin tokens.
type ServerStore interface {
	// AdminTokenHash returns the stored hash, or "" if no token has been
	// made yet.
	AdminTokenHash(ctx context.Context) (string, error)
	SetAdminTokenHash(ctx context.Context, hash string) error
}

// MemServer is the in-memory ServerStore used by tests.
type MemServer struct {
	mu   sync.RWMutex
	hash string
}

func NewMemServer() *MemServer {
	return &MemServer{}
}

func (m *MemServer) AdminTokenHash(_ context.Context) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hash, nil
}

func (m *MemServer) SetAdminTokenHash(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hash = hash
	return nil
}

// SQLiteServer keeps the server admin token hash in a one-row settings
// table in relay.db, so it survives restarts and only someone who can read
// the database file could replace it.
type SQLiteServer struct {
	db *sql.DB
}

func NewSQLiteServer(db *sql.DB) (*SQLiteServer, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS server (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
	if err != nil {
		return nil, err
	}
	return &SQLiteServer{db: db}, nil
}

const serverAdminKey = "admin_token_hash"

func (s *SQLiteServer) AdminTokenHash(ctx context.Context) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM server WHERE key = ?`, serverAdminKey,
	).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return hash, nil
}

func (s *SQLiteServer) SetAdminTokenHash(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO server (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		serverAdminKey, hash,
	)
	return err
}

// Both backends must keep implementing ServerStore.
var (
	_ ServerStore = (*MemServer)(nil)
	_ ServerStore = (*SQLiteServer)(nil)
)
