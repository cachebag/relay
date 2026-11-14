package agent

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Verifies that job timeout durations are formatted correctly.
// These values are used in the error message when a job times out.
func TestFormatJobTimeout(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{
		{1, "1s"},
		{5, "5s"},
		{60, "1m"},
		{600, "10m"},
		{3600, "1h"},
		{5400, "1h30m"},
		{3665, "1h1m5s"},
		{86400, "24h"},
	} {
		if got := formatJobTimeout(tc.seconds); got != tc.want {
			t.Errorf(
				"formatJobTimeout(%d) = %q, want %q",
				tc.seconds,
				got,
				tc.want,
			)
		}
	}
}

// Verifies that the agent reports a timeout as a failed job
// and can accept another job after the timed-out command stops.
func TestAgentTimeoutReportsFailureAndTakesNextJob(t *testing.T) {
	t.Setenv(helperEnv, "1")

	// Create a fake scheduler that provides two jobs.
	f := newFakeScheduler(t)
	var next atomic.Int32

	f.setNext(func(w http.ResponseWriter) {
		switch next.Add(1) {
		case 1:
			// The first job sleeps for 60 seconds but has a
			// one-second execution deadline.
			serveJob(store.Job{
				ID:             "slow",
				Command:        []string{os.Args[0], "sleep:60000"},
				TimeoutSeconds: 1,
				Attempts:       1,
			})(w)

		case 2:
			// The second job completes immediately, proving that
			// the agent can continue working after a timeout.
			serveJob(store.Job{
				ID:             "next",
				Command:        []string{os.Args[0], "out:next job"},
				TimeoutSeconds: 30,
				Attempts:       1,
			})(w)

		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	a := newTestAgent(f, "node-1", RunCommand)

	// The first job should be terminated shortly after one second. The bound
	// is loose because starting and killing a process on a busy Windows
	// runner has taken over 2s; it only has to be far below the 60s sleep.
	started := time.Now()
	tickAndWait(a)

	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf(
			"one-second timeout took %s, want under 10s",
			elapsed,
		)
	}

	// Verify the scheduler received a failed result.
	results := f.posted()

	if len(results) != 1 {
		t.Fatalf(
			"posted %d results, want 1",
			len(results),
		)
	}

	// A timed-out command must report exit code -1.
	if got := results[0].body["exit_code"]; got != float64(-1) {
		t.Errorf(
			"exit_code = %v, want -1",
			got,
		)
	}

	// The timeout message must appear at the end of stderr.
	if got, _ := results[0].body["stderr"].(string); !strings.HasSuffix(
		got,
		"relay: job timed out after 1s\n",
	) {
		t.Errorf(
			"stderr = %q, want timeout line at the end",
			got,
		)
	}

	// Once the failed result is reported, the agent
	// should be able to accept another job.
	tickAndWait(a)

	results = f.posted()

	if len(results) != 2 || results[1].jobID != "next" {
		t.Fatalf(
			"posted %+v, want the next job after the timeout",
			results,
		)
	}

	// The second job should complete successfully.
	if got := results[1].body["exit_code"]; got != float64(0) {
		t.Errorf(
			"second exit_code = %v, want 0",
			got,
		)
	}
}

// Verifies that timing out a command also terminates the
// child processes it spawned.
//
// This prevents a timed-out job from leaving background
// processes running on the contributing machine.
func TestRunCommandDeadlineKillsChildProcess(t *testing.T) {
	t.Setenv(helperEnv, "1")

	// The child process writes this file if it survives long enough.
	marker := filepath.Join(
		t.TempDir(),
		"child-survived.txt",
	)

	// Set a one-second execution deadline.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	// Start a command that spawns a child process.
	started := time.Now()

	res := RunCommand(
		ctx,
		[]string{os.Args[0], "spawnchild:" + marker},
		"",
	)

	// The command and its children should stop shortly
	// after the one-second deadline expires.
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf(
			"process-tree timeout took %s, want under 2s",
			elapsed,
		)
	}

	// The command must have started its child before
	// being terminated by the deadline.
	if res.ExitCode != -1 || !strings.Contains(res.Stdout, "spawned") {
		t.Fatalf(
			"result = %+v, want a started child and exit code -1",
			res,
		)
	}

	// If the child escaped termination, it would write a file
	// approximately two seconds after starting.
	time.Sleep(2300 * time.Millisecond)

	// Verify the marker file does not exist.
	// Its absence confirms that the child was terminated.
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("timed-out job left a child process running")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(
			"checking child marker: %v",
			err,
		)
	}
}
