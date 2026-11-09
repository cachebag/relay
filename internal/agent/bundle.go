package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cachebag/relay/internal/store"
	"github.com/cachebag/relay/internal/tarball"
)

const (
	// outputDir is the folder inside a job's working folder whose contents
	// go back to the scheduler with the result.
	outputDir = "out"
	// outputTarName is where the packed out/ waits inside the job folder
	// until the result is accepted.
	outputTarName = ".relay-out.tgz"
	// maxUnpackedBytes stops a small bundle from unpacking into a huge one.
	maxUnpackedBytes = 1 << 30
	// transferTimeout is for bundle downloads and uploads, which can be
	// 50 MB; the normal 5 s client is for heartbeats.
	transferTimeout = 10 * time.Minute
)

// maxOutputBundleBytes is store.MaxBundleBytes; a var so tests can lower it.
var maxOutputBundleBytes int64 = store.MaxBundleBytes

// errBundleRetry means the download failed in a way worth retrying: the
// scheduler was unreachable or answered 5xx. The job is left as it is, and
// the scheduler hands it back on the next poll.
var errBundleRetry = errors.New("can't fetch job files right now")

// transferClient is the agent's client with a timeout fit for a bundle.
func (a *Agent) transferClient() *http.Client {
	return &http.Client{Transport: a.client.Transport, Timeout: transferTimeout}
}

// fetchBundle downloads job's files from the scheduler and unpacks them
// into dir.
func (a *Agent) fetchBundle(ctx context.Context, job store.Job, dir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.schedulerURL+"/jobs/"+url.PathEscape(job.ID)+"/bundle?node="+url.QueryEscape(a.node.ID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.poolToken)
	resp, err := a.transferClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errBundleRetry, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: scheduler returned %d", errBundleRetry, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("scheduler returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := tarball.Unpack(resp.Body, dir, maxUnpackedBytes); err != nil {
		return fmt.Errorf("unpacking job files: %w", err)
	}
	return nil
}

// entryArgv builds the command line for a bundle job: the interpreter for
// the entry's file type, the entry's path inside dir, then the job's
// arguments. With no entry named, it is run.sh on Linux and macOS and
// run.ps1 or run.cmd on Windows, whichever the bundle has.
func entryArgv(entry, dir string, args []string, goos string) ([]string, error) {
	if entry == "" {
		candidates := []string{"run.sh"}
		if goos == "windows" {
			candidates = []string{"run.ps1", "run.cmd"}
		}
		for _, c := range candidates {
			if _, err := os.Stat(filepath.Join(dir, c)); err == nil {
				entry = c
				break
			}
		}
		if entry == "" {
			return nil, fmt.Errorf("job files have no %s to run on %s", strings.Join(candidates, " or "), goos)
		}
	}
	p := filepath.Join(dir, filepath.FromSlash(entry))
	if _, err := os.Stat(p); err != nil {
		return nil, fmt.Errorf("entry %s is not in the job files", entry)
	}
	var argv []string
	switch strings.ToLower(filepath.Ext(p)) {
	case ".sh":
		argv = []string{"sh", p}
	case ".py":
		if goos == "windows" {
			argv = []string{"python", p}
		} else {
			argv = []string{"python3", p}
		}
	case ".ps1":
		argv = []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", p}
	case ".cmd", ".bat":
		argv = []string{"cmd", "/c", p}
	default:
		// A program of its own. The tar may have lost the executable bit
		// on the way through Windows, so set it.
		if goos != "windows" {
			os.Chmod(p, 0o755)
		}
		argv = []string{p}
	}
	return append(argv, args...), nil
}

// packOutput tars dir/out into dir/.relay-out.tgz. It returns the tar's
// path and size, or "" when out/ holds no files.
func packOutput(dir string) (string, int64, error) {
	out := filepath.Join(dir, outputDir)
	files := 0
	err := filepath.WalkDir(out, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			files++
		}
		return err
	})
	if errors.Is(err, fs.ErrNotExist) || files == 0 {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	path := filepath.Join(dir, outputTarName)
	f, err := os.Create(path)
	if err != nil {
		return "", 0, err
	}
	err = tarball.Pack(f, out)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	return path, info.Size(), nil
}
