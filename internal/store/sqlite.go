package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"modernc.org/sqlite"
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

	// SQLite allows one writer at a time, and busy_timeout is not a fair
	// queue: under enough concurrent writes one can lose every retry and fail
	// with SQLITE_BUSY after the full 5 s. A single connection makes
	// database/sql queue callers instead, so SQLite never sees contention.
	// Nothing here holds a connection across calls (every query closes its
	// rows before returning), so this can't deadlock.
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS nodes (
		id        TEXT PRIMARY KEY,
		hostname  TEXT NOT NULL,
		os        TEXT NOT NULL,
		arch      TEXT NOT NULL,
		cpus      INTEGER NOT NULL,
		mem_bytes INTEGER NOT NULL,
		last_seen INTEGER NOT NULL,
		pool_id   TEXT NOT NULL DEFAULT '',
		owner     TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}

	_, _ = db.Exec(`ALTER TABLE nodes ADD COLUMN pool_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE nodes ADD COLUMN owner TEXT NOT NULL DEFAULT ''`)

	// Machines a pool admin removed. One row per (pool, node), so removing
	// the same machine twice keeps a single tombstone.
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS tombstones (
    		pool_id    TEXT NOT NULL,
    		node_id    TEXT NOT NULL,
    		created_at INTEGER NOT NULL,
			token_hash TEXT NOT NULL DEFAULT '',
    		PRIMARY KEY (pool_id, node_id)
    	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateTombstoneTokenHash(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLite{db: db}, nil
}

// backupper is the driver interface modernc.org/sqlite implements.
type backupper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

// Backup writes an online snapshot of this live database to destPath.
// The destination is created by SQLite, so destPath must not already exist.
func (s *SQLite) Backup(ctx context.Context, destPath string) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	return conn.Raw(func(driverConn any) error {
		driver, ok := driverConn.(backupper)
		if !ok {
			return fmt.Errorf("database driver does not support backup")
		}

		backup, err := driver.NewBackup(destPath)
		if err != nil {
			return err
		}

		more, err := backup.Step(-1)
		if err != nil {
			_ = backup.Finish()
			return err
		}
		if more {
			_ = backup.Finish()
			return fmt.Errorf("backup did not finish")
		}

		return backup.Finish()
	})
}

func (s *SQLite) Upsert(_ context.Context, n Node) error {
	result, err := s.db.Exec(
		`INSERT INTO nodes (id, hostname, os, arch, cpus, mem_bytes, last_seen, pool_id, owner)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   hostname=excluded.hostname,
		   os=excluded.os,
		   arch=excluded.arch,
		   cpus=excluded.cpus,
		   mem_bytes=excluded.mem_bytes,
		   last_seen=excluded.last_seen,
		   pool_id=excluded.pool_id,
		   owner=excluded.owner
		 WHERE nodes.pool_id=excluded.pool_id`,
		n.ID, n.Hostname, n.OS, n.Arch, n.CPUs, n.MemBytes, n.LastSeen.Unix(), n.PoolID, n.Owner,
	)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrPoolConflict
	}
	return nil
}

func (s *SQLite) List(_ context.Context) ([]Node, error) {
	rows, err := s.db.Query(`SELECT id, hostname, os, arch, cpus, mem_bytes, last_seen, pool_id, owner FROM nodes ORDER BY hostname, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Node, 0)
	for rows.Next() {
		var n Node
		var sec int64
		if err := rows.Scan(&n.ID, &n.Hostname, &n.OS, &n.Arch, &n.CPUs, &n.MemBytes, &sec, &n.PoolID, &n.Owner); err != nil {
			return nil, err
		}
		n.LastSeen = time.Unix(sec, 0).UTC()
		out = append(out, n)
	}

	return out, rows.Err()
}

// Delete removes the node with the given ID, or returns ErrNotFound.
func (s *SQLite) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) DB() *sql.DB {
	return s.db
}

// Windows locks open files, so TempDir cleanup fails without this.
func (s *SQLite) Close() error {
	return s.db.Close()
}
