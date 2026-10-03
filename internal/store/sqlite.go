package store

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLite, error) {
	db, err := sql.Open(
		"sqlite",
		"file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
	)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS nodes (
		id        TEXT PRIMARY KEY,
		hostname  TEXT NOT NULL,
		os        TEXT NOT NULL,
		arch      TEXT NOT NULL,
		cpus      INTEGER NOT NULL,
		mem_bytes INTEGER NOT NULL,
		last_seen INTEGER NOT NULL,
		pool_id   TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	_, _ = db.Exec(`ALTER TABLE nodes ADD COLUMN pool_id TEXT NOT NULL DEFAULT ''`)

	return &SQLite{db: db}, nil
}

func (s *SQLite) Upsert(_ context.Context, n Node) error {
	_, err := s.db.Exec(
		`INSERT INTO nodes (id, hostname, os, arch, cpus, mem_bytes, last_seen, pool_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   hostname=excluded.hostname,
		   os=excluded.os,
		   arch=excluded.arch,
		   cpus=excluded.cpus,
		   mem_bytes=excluded.mem_bytes,
		   last_seen=excluded.last_seen,
		   pool_id=excluded.pool_id`,
		n.ID, n.Hostname, n.OS, n.Arch, n.CPUs, n.MemBytes, n.LastSeen.Unix(), n.PoolID,
	)
	return err
}

func (s *SQLite) List(_ context.Context) ([]Node, error) {
	rows, err := s.db.Query(`SELECT id, hostname, os, arch, cpus, mem_bytes, last_seen, pool_id FROM nodes ORDER BY hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Node, 0)
	for rows.Next() {
		var n Node
		var sec int64
		if err := rows.Scan(&n.ID, &n.Hostname, &n.OS, &n.Arch, &n.CPUs, &n.MemBytes, &sec, &n.PoolID); err != nil {
			return nil, err
		}
		n.LastSeen = time.Unix(sec, 0).UTC()
		out = append(out, n)
	}

	return out, rows.Err()
}

func (s *SQLite) DB() *sql.DB {
	return s.db
}

// Windows locks open files, so TempDir cleanup fails without this.
func (s *SQLite) Close() error {
	return s.db.Close()
}
