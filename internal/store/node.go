package store

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors every backend returns, so callers can use errors.Is instead
// of matching on driver-specific error text.
var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

// Node is a single machine known to the control plane. The JSON tags are part
// of the wire format the agents and the API share, so renaming a field here is
// a breaking change for anything already on the network.
type Node struct {
	ID       string    `json:"id"`
	PoolID   string    `json:"pool_id"`
	Hostname string    `json:"hostname"`
	OS       string    `json:"os"`
	Arch     string    `json:"arch"`
	CPUs     int       `json:"cpus"`
	MemBytes int64     `json:"mem_bytes"`
	LastSeen time.Time `json:"last_seen"`
}

// NodeTimeout is how long a node may go without a heartbeat before it is
// treated as offline. It lives here rather than in the scheduler so every
// caller (scheduler, API, tests) shares one definition of "online".
const NodeTimeout = 30 * time.Second

// Online reports whether n has heartbeated within NodeTimeout of now.
func Online(n Node, now time.Time) bool {
	return now.Sub(n.LastSeen) < NodeTimeout
}

// Registry is the storage contract every backend implements: Mem here, SQLite
// next. Keeping it this narrow means callers never depend on which one
// they got, and the shared test in registry_test.go can exercise any of them.
//
// Implementations must satisfy two guarantees the callers rely on:
//   - Upsert keys on Node.ID, so re-registering an existing node overwrites its
//     record rather than adding a duplicate.
//   - List returns a stable ordering, so repeated calls don't shuffle output.
//
// ctx is part of the signature for backends that do real I/O; Mem ignores it.
type Registry interface {
	Upsert(ctx context.Context, n Node) error
	List(ctx context.Context) ([]Node, error)
}
