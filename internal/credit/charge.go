package credit

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// ReasonJob marks a ledger entry charged for a job's run time.
const ReasonJob = "job"

// Charge bills the submitter of j for the time it ran, 1 credit per minute
// rounded up and never less than 1. It needs both timestamps and a
// submitter, so a job the scheduler gave up on (never reported, no finish
// from a node) or one from before submitters existed is not charged. A job
// whose command couldn't be started (exit code -1) used nothing, so it
// isn't charged either.
func Charge(ctx context.Context, credits store.CreditStore, j store.Job) error {
	if j.Submitter == "" || j.StartedAt == nil || j.FinishedAt == nil || j.ExitCode == -1 {
		return nil
	}

	minutes := int(math.Ceil(j.FinishedAt.Sub(*j.StartedAt).Minutes()))
	if minutes < 1 {
		minutes = 1
	}

	err := credits.Add(ctx, store.CreditEntry{
		ID:        store.NewID(),
		PoolID:    j.PoolID,
		Account:   j.Submitter,
		Delta:     -minutes,
		Reason:    ReasonJob,
		JobID:     j.ID,
		CreatedAt: j.FinishedAt.UTC(),
	})
	if err != nil {
		return err
	}
	log.Printf("charged %s %d credits in pool %s: job %s ran %s", j.Submitter, minutes, j.PoolID, j.ID, Runtime(j))
	return nil
}

// Runtime is how long j ran, or has been running, for logs.
func Runtime(j store.Job) string {
	if j.StartedAt == nil {
		return "0s"
	}
	end := time.Now()
	if j.FinishedAt != nil {
		end = *j.FinishedAt
	}
	return fmt.Sprint(end.Sub(*j.StartedAt).Round(time.Second))
}
