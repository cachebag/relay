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
// finish, no submitter, or a command that couldn't start adds nothing to the
// ledger.
func TestChargeSkipsUnfinishedOrAnonymous(t *testing.T) {
	full := chargedJob(0, time.Minute)
	noStart, noFinish, noName, noLaunch := full, full, full, full
	noStart.StartedAt = nil
	noFinish.FinishedAt = nil
	noName.Submitter = ""
	noLaunch.Status = store.JobFailed
	noLaunch.ExitCode = -1
	noLaunch.Stderr = "exec: command not found\n"

	for name, j := range map[string]store.Job{"no start": noStart, "no finish": noFinish, "no submitter": noName, "couldn't start": noLaunch} {
		credits := store.NewMemCredits()
		if err := Charge(context.Background(), credits, j); err != nil {
			t.Fatalf("%s: Charge: %v", name, err)
		}
		if got, _ := credits.Balances(context.Background(), "pool-a"); len(got) != 0 {
			t.Errorf("%s: expected no entries, got %v", name, got)
		}
	}
}

// TestChargeBillsTimedOutJob verifies that jobs terminated by their execution
// deadline are charged even though they report exit code -1.
func TestChargeBillsTimedOutJob(t *testing.T) {
	cases := []struct {
		run     time.Duration
		timeout string
		want    int
	}{
		{5 * time.Second, "5s", -1},
		{time.Hour, "1h", -60},
	}

	for _, c := range cases {
		j := chargedJob(0, c.run)
		j.Status = store.JobFailed
		j.ExitCode = -1
		j.Stderr = "command output\nrelay: job timed out after " + c.timeout + "\n"

		credits := store.NewMemCredits()

		if err := Charge(context.Background(), credits, j); err != nil {
			t.Fatalf("Charge: %v", err)
		}

		got, err := credits.Balances(context.Background(), "pool-a")
		if err != nil {
			t.Fatalf("Balances: %v", err)
		}

		if got["akrm"] != c.want {
			t.Errorf("timed-out job ran %s: balance = %d, want %d",
				c.run, got["akrm"], c.want)
		}
	}
}
