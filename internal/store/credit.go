package store

import (
	"context"
	"time"
)

// CreditEntry is one change to an account's balance in a pool. Entries are
// only ever added, never edited, so a balance is just the sum of an account's
// entries and the history behind it stays visible.
type CreditEntry struct {
	ID        string    `json:"id"`
	PoolID    string    `json:"pool_id"`
	Account   string    `json:"account"`
	Delta     int       `json:"delta"`
	Reason    string    `json:"reason"`
	JobID     string    `json:"job_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreditStore is the storage contract for the credit ledger. Implementations
// must hold to what the shared test in creditstore_test.go checks:
//   - Add returns ErrExists if the ID is already taken.
//   - Balances only counts entries in the given pool, and returns an empty
//     map for a pool with no entries.
type CreditStore interface {
	Add(ctx context.Context, e CreditEntry) error
	Balances(ctx context.Context, poolID string) (map[string]int, error)
}
