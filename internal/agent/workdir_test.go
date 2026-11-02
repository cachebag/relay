package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// Folders left by a crash are cleared when the agent starts, except the one
// A job ID that isn't a plain name never becomes a path.
func TestWorkdirRejectsPathLikeIDs(t *testing.T) {
	a := &Agent{cacheDir: t.TempDir()}
	for _, id := range []string{"", ".", "..", "../x", `a\b`, "a/b", "C:x"} {
		if _, err := a.workdir(id); err == nil {
			t.Errorf("workdir(%q) accepted a path-like ID", id)
		}
	}
}
