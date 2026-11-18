package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ErrNoBubblewrap is FindSandbox's error when bwrap is not installed.
var ErrNoBubblewrap = errors.New("no bubblewrap found")

// Sandbox runs a job inside bubblewrap. The job sees /usr and /etc read-only
// and its own folder read-write, and nothing else of the machine: no home
// folder, no other job's folder, no agent files and no network.
type Sandbox struct {
	bwrap string
	// root is the bwrap arguments for the top-level names that lead into
	// /usr (/bin, /lib and so on), which differ from one distro to the next.
	root []string
}

// FindSandbox returns the sandbox this machine can run jobs in, or an error
// that says why there is none. Only Linux has one; elsewhere both are nil.
func FindSandbox() (*Sandbox, error) {
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, ErrNoBubblewrap
	}
	s := &Sandbox{bwrap: bwrap, root: rootArgs()}

	// bwrap can be installed and still refused, where unprivileged user
	// namespaces are turned off, so run nothing in one before promising it.
	dir, err := os.MkdirTemp("", "relay-sandbox-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if res := RunCommand(ctx, s.Argv([]string{"true"}, dir), dir); res.ExitCode != 0 {
		why := strings.TrimSpace(res.Stderr)
		if i := strings.LastIndex(why, "\n"); i >= 0 {
			why = why[i+1:]
		}
		return nil, fmt.Errorf("bubblewrap can't start a sandbox here (%s)", why)
	}
	return s, nil
}

// rootArgs keeps each of the usual top-level names as it is on this machine:
// a symlink into /usr where the distro has one, a read-only bind where it is
// a real folder.
func rootArgs() []string {
	var args []string
	for _, path := range []string{"/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32"} {
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(path); err == nil {
				args = append(args, "--symlink", target, path)
			}
		} else if info.IsDir() {
			args = append(args, "--ro-bind", path, path)
		}
	}
	return args
}

// Argv is the bwrap command line that runs argv in dir. dir keeps its path
// inside the sandbox, so the entry script and HOME mean the same in both.
func (s *Sandbox) Argv(argv []string, dir string) []string {
	out := []string{
		s.bwrap,
		"--unshare-all", "--die-with-parent", "--new-session",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/etc", "/etc",
	}
	out = append(out, s.root...)
	// /tmp comes before dir, which may be under it.
	out = append(out,
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--bind", dir, dir,
		"--chdir", dir,
		"--",
	)
	return append(out, argv...)
}

// Run is RunCommand inside the sandbox.
func (s *Sandbox) Run(ctx context.Context, argv []string, dir string) Result {
	res := RunCommand(ctx, s.Argv(argv, dir), dir)
	// bwrap exits 1 when the command isn't there to run. Outside a sandbox
	// that is exit code -1, which is how the scheduler tells a job that
	// never started from one that failed.
	if why, ok := strings.CutPrefix(res.Stderr, "bwrap: execvp "); ok && res.ExitCode == 1 {
		return Result{ExitCode: -1, Stderr: "exec: " + why}
	}
	return res
}
