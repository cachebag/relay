package credit

import (
	"context"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

func chargedJob(start, run time.Duration) store.Job {
	base := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	s := base.Add(start)
	f := s.Add(run)
	return store.Job{ID: "job-1", PoolID: "pool-a", Submitter: "akrm", Status: store.JobDone, StartedAt: &s, FinishedAt: &f}
}

// TestChargeRoundsUp checks the per-minute billing: 90 s is 2 credits,
// 5 s is still 1, and exactly 3 minutes is 3.
func TestChargeRoundsUp(t *testing.T) {
	cases := []struct {
		run  time.Duration
		want int
	}{
		{90 * time.Second, -2},
		{5 * time.Second, -1},
		{3 * time.Minute, -3},
	}
	for _, c := range cases {
		credits := store.NewMemCredits()
		if err := Charge(context.Background(), credits, chargedJob(0, c.run)); err != nil {
			t.Fatalf("Charge: %v", err)
		}
		got, _ := credits.Balances(context.Background(), "pool-a")
		if got["akrm"] != c.want {
			t.Errorf("run %s: balance = %d, want %d", c.run, got["akrm"], c.want)
		}
	}
}

// TestChargeSkipsUnfinishedOrAnonymous checks that a job with no start, no
// finish, or no submitter adds nothing to the ledger.
func TestChargeSkipsUnfinishedOrAnonymous(t *testing.T) {
	full := chargedJob(0, time.Minute)
	noStart, noFinish, noName := full, full, full
	noStart.StartedAt = nil
	noFinish.FinishedAt = nil
	noName.Submitter = ""

	for name, j := range map[string]store.Job{"no start": noStart, "no finish": noFinish, "no submitter": noName} {
		credits := store.NewMemCredits()
		if err := Charge(context.Background(), credits, j); err != nil {
			t.Fatalf("%s: Charge: %v", name, err)
		}
		if got, _ := credits.Balances(context.Background(), "pool-a"); len(got) != 0 {
			t.Errorf("%s: expected no entries, got %v", name, got)
		}
	}
}
