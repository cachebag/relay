package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// jobsDir holds one folder per job, inside the agent's cache directory.
const jobsDir = "jobs"

// workRoot is where job folders go. Without a cache directory they go in the
// system temp folder instead, so a job still never runs where the agent
// was started.
func (a *Agent) workRoot() string {
	if a.cacheDir == "" {
		return filepath.Join(os.TempDir(), "relay-jobs")
	}
	return filepath.Join(a.cacheDir, jobsDir)
}

// workdir is the folder for job id. The ID comes from the scheduler, so
// anything that isn't a single plain name is refused rather than joined
// into a path that could point outside the jobs folder.
func (a *Agent) workdir(id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\:`) {
		return "", fmt.Errorf("bad job id %q", id)
	}
	return filepath.Join(a.workRoot(), id), nil
}

// makeWorkdir creates an empty folder for job id, with a tmp folder inside
// it for TMPDIR. Anything left there by an earlier attempt is removed first.
func (a *Agent) makeWorkdir(id string) (string, error) {
	dir, err := a.workdir(id)
	if err != nil {
		return "", err
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// removeWorkdir deletes job id's folder.
func (a *Agent) removeWorkdir(id string) {
	dir, err := a.workdir(id)
	if err != nil {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("removing job folder %s: %v", dir, err)
	}
}

// cleanWorkdirs deletes job folders left behind by a crash. The folder of a
// result still waiting to be reported is kept until it is. The shared temp
// fallback is left alone, since other agents may be using it.
func (a *Agent) cleanWorkdirs() {
	if a.cacheDir == "" {
		return
	}
	entries, err := os.ReadDir(a.workRoot())
	if err != nil {
		return
	}
	for _, e := range entries {
		if a.pending != nil && e.Name() == a.pending.JobID {
			continue
		}
		if err := os.RemoveAll(filepath.Join(a.workRoot(), e.Name())); err != nil {
			log.Printf("removing leftover job folder %s: %v", e.Name(), err)
		}
	}
}
