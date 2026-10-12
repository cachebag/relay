package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/credit"
	"github.com/cachebag/relay/internal/store"
)

// ledger is a CreditStore that keeps every entry so tests can check each
// payment, not just the balances.
type ledger struct {
	entries []store.CreditEntry
}

func (l *ledger) Add(_ context.Context, e store.CreditEntry) error {
	l.entries = append(l.entries, e)
	return nil
}

func (l *ledger) Balances(_ context.Context, poolID string) (map[string]int, error) {
	out := make(map[string]int)
	for _, e := range l.entries {
		if e.PoolID == poolID {
			out[e.Account] += e.Delta
		}
	}
	return out, nil
}

// paid returns the deltas paid to account in pool, in order.
func (l *ledger) paid(pool, account string) []int {
	var out []int
	for _, e := range l.entries {
		if e.PoolID == pool && e.Account == account {
			out = append(out, e.Delta)
		}
	}
	return out
}

// Test:
// Two nodes in different pools heartbeat every minute for 25 minutes, then
// stop. Each owner should be paid 10, 10 while online and 5 once their node
// goes offline, in their own pool only.
func TestOnlineTimeEarnsCredits(t *testing.T) {
	ctx := context.Background()
	nodes := store.NewMem()
	l := &ledger{}
	s := New(nodes, store.NewMemJobs(), credit.NewEarner(l))

	start := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	heartbeat := func(now time.Time) {
		for _, n := range []store.Node{
			{ID: "node-a", PoolID: "pool-a", Owner: "akrm", LastSeen: now},
			{ID: "node-b", PoolID: "pool-b", Owner: "omar", LastSeen: now},
		} {
			if err := nodes.Upsert(ctx, n); err != nil {
				t.Fatalf("upsert: %v", err)
			}
		}
	}

	for m := 0; m <= 25; m++ {
		now := start.Add(time.Duration(m) * time.Minute)
		heartbeat(now)
		if err := s.Tick(ctx, now); err != nil {
			t.Fatalf("Tick at %dm: %v", m, err)
		}
	}

	if got := l.paid("pool-a", "akrm"); len(got) != 2 || got[0] != 10 || got[1] != 10 {
		t.Fatalf("after 25 online minutes: want [10 10], got %v", got)
	}

	// No heartbeat for a minute, so both nodes are offline now.
	if err := s.Tick(ctx, start.Add(26*time.Minute)); err != nil {
		t.Fatalf("Tick after going offline: %v", err)
	}

	for _, c := range []struct{ pool, account string }{
		{"pool-a", "akrm"},
		{"pool-b", "omar"},
	} {
		got := l.paid(c.pool, c.account)
		if len(got) != 3 || got[0] != 10 || got[1] != 10 || got[2] != 5 {
			t.Errorf("%s in %s: want [10 10 5], got %v", c.account, c.pool, got)
		}
	}
	if got := l.paid("pool-a", "omar"); len(got) != 0 {
		t.Errorf("omar paid in pool-a: %v", got)
	}
	if got := l.paid("pool-b", "akrm"); len(got) != 0 {
		t.Errorf("akrm paid in pool-b: %v", got)
	}

	// Offline nodes are forgotten, so another tick pays nothing more.
	if err := s.Tick(ctx, start.Add(27*time.Minute)); err != nil {
		t.Fatalf("Tick while offline: %v", err)
	}
	if len(l.entries) != 6 {
		t.Errorf("want 6 entries in total, got %d", len(l.entries))
	}
}

// Test:
// A node with no owner is online for 15 minutes. Nobody gets paid.
func TestNoOwnerEarnsNothing(t *testing.T) {
	ctx := context.Background()
	nodes := store.NewMem()
	l := &ledger{}
	s := New(nodes, store.NewMemJobs(), credit.NewEarner(l))

	start := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for m := 0; m <= 15; m++ {
		now := start.Add(time.Duration(m) * time.Minute)
		if err := nodes.Upsert(ctx, store.Node{ID: "node-a", PoolID: "pool-a", LastSeen: now}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := s.Tick(ctx, now); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}

	if len(l.entries) != 0 {
		t.Errorf("want no entries, got %+v", l.entries)
	}
}
