package store

import (
	"context"
	"time"
)

// TombstoneTTL defines how long a tombstone remains valid. After this period,
// the tombstone can be purged.
const TombstoneTTL = 30 * 24 * time.Hour // 30 days

// Tombstone records that a pool admin removed a machine from a pool. The
// heartbeat handler checks for one before saving anything, so a machine
// using a removed join token is refused instead of quietly registering itself again.
type Tombstone struct {
	PoolID    string    `json:"pool_id"`
	NodeID    string    `json:"node_id"`
	CreatedAt time.Time `json:"created_at"`
	TokenHash string    `json:"token_hash"`
}

// Tombstones is the storage contract for removed machines. Mem and SQLite
// both implement it next to Registry, so the API reaches it through the
// registry it already has. Purging tombstones after 30 days (FR-30) is not
// done yet.
//
// Implementations must hold to what the shared test in tombstone_test.go
// checks:
//   - AddTombstone for a (pool, node) pair that already has one succeeds
//     and keeps a single tombstone, updating its token hash and timestamp.
//   - HasTombstone only matches the exact pool and node, so a machine
//     removed from one pool can still heartbeat into another.
//   - DeleteTombstone succeeds even if the tombstone does not exist.
type Tombstones interface {
	AddTombstone(ctx context.Context, t Tombstone) error
	HasTombstone(ctx context.Context, poolID, nodeID string) (bool, error)
	GetTombstone(ctx context.Context, poolID, nodeID string) (Tombstone, bool, error)
	DeleteTombstone(ctx context.Context, poolID, nodeID string) error
}

// Both backends must keep implementing Tombstones.
var (
	_ Tombstones = (*Mem)(nil)
	_ Tombstones = (*SQLite)(nil)
)
