package store

import "context"

// tombstoneKey identifies a removed machine within one pool.
type tombstoneKey struct {
	poolID string
	nodeID string
}

// AddTombstone records that t.NodeID was removed from t.PoolID. Adding one
// that already exists just replaces it.
func (m *Mem) AddTombstone(_ context.Context, t Tombstone) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// NewMem doesn't create this map, so make it on first use.
	if m.tombstones == nil {
		m.tombstones = make(map[tombstoneKey]Tombstone)
	}
	m.tombstones[tombstoneKey{t.PoolID, t.NodeID}] = t
	return nil
}

// HasTombstone reports whether nodeID was removed from poolID.
func (m *Mem) HasTombstone(_ context.Context, poolID, nodeID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Reading a nil map is fine, so no tombstones yet just means false.
	_, ok := m.tombstones[tombstoneKey{poolID, nodeID}]
	return ok, nil
}