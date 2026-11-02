package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// A job runs in <cache>/jobs/<id> with HOME pointed there, and the folder is
// gone once the scheduler has accepted the result.
func TestJobRunsInItsOwnFolder(t *testing.T) {
	t.Setenv(helperEnv, "1")
	f := newFakeScheduler(t)
	// Resolve the temp folder first, so the job's working directory compares
	// equal on macOS (/var is a symlink) and Windows (short names).
	cacheDir, err := filepath.EvalSymlinks(f.cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	f.cacheDir = cacheDir
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{os.Args[0], "pwd", "env:HOME"}}))
	a := newTestAgent(f, "node-1", RunCommand)

	tickAndWait(a)

	results := f.posted()
	if len(results) != 1 {
		t.Fatalf("posted %d results, want 1", len(results))
	}
	stdout, _ := results[0].body["stdout"].(string)
	want := filepath.Join(cacheDir, jobsDir, "job-1")
	if got := strings.Fields(stdout); len(got) != 2 || got[0] != want || got[1] != want {
		t.Errorf("working directory and HOME = %q, want both %q", got, want)
	}

	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Errorf("job folder still there after the result was accepted: %v", err)
	}
}

// With KeepWorkdir the folder is left in place after the result is reported.
func TestKeepWorkdirLeavesFolder(t *testing.T) {
	f := newFakeScheduler(t)
	f.setNext(serveJob(store.Job{ID: "job-1", Command: []string{"true"}}))
	a := newTestAgent(f, "node-1", (&FakeRunner{}).run)
	a.KeepWorkdir()

	tickAndWait(a)

	if len(f.posted()) != 1 {
		t.Fatal("result was not reported")
	}
	if _, err := os.Stat(filepath.Join(f.cacheDir, jobsDir, "job-1")); err != nil {
		t.Errorf("job folder removed despite KeepWorkdir: %v", err)
	}
}

// Folders left by a crash are cleared when the agent starts, except the one
// for a result that hasn't been reported yet.
func TestLeftoverWorkdirsClearedOnStart(t *testing.T) {
	f := newFakeScheduler(t)
	jobs := filepath.Join(f.cacheDir, jobsDir)
	for _, id := range []string{"crashed", "unreported"} {
		if err := os.MkdirAll(filepath.Join(jobs, id), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	newTestAgent(f, "node-1", nil).setPending(pending{JobID: "unreported", Attempt: 1})

	a := newTestAgent(f, "node-1", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.Run(ctx, time.Hour)

	if _, err := os.Stat(filepath.Join(jobs, "crashed")); !os.IsNotExist(err) {
		t.Errorf("leftover job folder not removed on start: %v", err)
	}
	if _, err := os.Stat(filepath.Join(jobs, "unreported")); err != nil {
		t.Errorf("folder of an unreported result was removed: %v", err)
	}
}

// A job ID that isn't a plain name never becomes a path.
func TestWorkdirRejectsPathLikeIDs(t *testing.T) {
	a := &Agent{cacheDir: t.TempDir()}
	for _, id := range []string{"", ".", "..", "../x", `a\b`, "a/b", "C:x"} {
		if _, err := a.workdir(id); err == nil {
			t.Errorf("workdir(%q) accepted a path-like ID", id)
		}
	}
}
