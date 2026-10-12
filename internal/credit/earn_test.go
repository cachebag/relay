package credit

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Test:
// A machine is online for 7 minutes as akrm, then restarts with a different
// owner. akrm should be paid the 7 minutes straight away, and the new owner
// should start from zero.
func TestOwnerChangeSettlesUp(t *testing.T) {
	ctx := context.Background()
	credits := store.NewMemCredits()
	e := NewEarner(credits)

	start := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	node := store.Node{ID: "node-a", PoolID: "pool-a", Owner: "akrm"}

	for m := 0; m <= 7; m++ {
		if err := e.Tick(ctx, start.Add(time.Duration(m)*time.Minute), []store.Node{node}); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}

	node.Owner = "omar"
	if err := e.Tick(ctx, start.Add(8*time.Minute), []store.Node{node}); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, err := credits.Balances(ctx, "pool-a")
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if got["akrm"] != 7 || got["omar"] != 0 {
		t.Errorf("want akrm 7 and omar 0, got %v", got)
	}
}
