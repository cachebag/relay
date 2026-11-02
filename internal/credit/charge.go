package credit

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// ReasonJob marks a ledger entry charged for a job's run time.
const ReasonJob = "job"

// Charge bills the submitter of j for the time it ran, 1 credit per minute
// rounded up and never less than 1. Jobs without a submitter or complete
// timestamps are not charged. Commands that never launched are also free,
// but jobs terminated by their execution deadline are charged.
func Charge(ctx context.Context, credits store.CreditStore, j store.Job) error {
	if j.Submitter == "" || j.StartedAt == nil || j.FinishedAt == nil {
		return nil
	}

	// Exit code -1 can mean either a launch failure or an execution timeout.
	// Only skip billing when the command failed to launch.
	if j.ExitCode == -1 && !timedOut(j) {
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

// timedOut checks for the timeout message appended by the agent.
// The message must be the last line of stderr.
func timedOut(j store.Job) bool {
	stderr := strings.TrimRight(j.Stderr, "\r\n")
	lastLine := stderr[strings.LastIndex(stderr, "\n")+1:]
	return strings.HasPrefix(lastLine, "relay: job timed out after ")
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
