package agent

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

const (
	// maxOutput is how much of each stream is kept.
	maxOutput = 1 << 20

	truncatedMarker = "\n[output truncated]\n"
)

// Runs argv directly (no shell) as the agent's user, with no
// sandbox, and captures stdout and stderr separately. The exit code comes
// from the process; if it can't be started at all, the exit code is -1 and
// stderr holds the error text.
func RunCommand(ctx context.Context, argv []string) Result {
	if len(argv) == 0 {
		return Result{ExitCode: -1, Stderr: "empty command"}
	}

	var stdout, stderr CapWriter
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	configureJobProcess(cmd)

	// Cancel the entire process tree rather than only the immediate process.
	cmd.Cancel = func() error {
		return killJobProcessTree(cmd)
	}
	cmd.WaitDelay = 500 * time.Millisecond

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// A canceled or timed-out command is reported with exit code -1.
	// Preserve any output captured before the command stopped.
	if ctx.Err() != nil {
		return Result{ExitCode: -1, Stdout: stdout.String(), Stderr: stderr.String()}
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return Result{Stdout: stdout.String(), Stderr: stderr.String()}
	case errors.As(err, &exitErr):
		return Result{
			ExitCode: exitErr.ExitCode(),
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		}
	default:
		return Result{ExitCode: -1, Stdout: stdout.String(), Stderr: err.Error() + "\n"}
	}
}

// CapWriter keeps the first maxOutput bytes written to it and drops the rest.
// It never returns an error, so the process keeps running (and its pipe keeps
// draining) even when it produces far more output than we keep.
type CapWriter struct {
	buf       bytes.Buffer
	truncated bool
}

func (w *CapWriter) Write(p []byte) (int, error) {
	n := len(p)
	if room := maxOutput - w.buf.Len(); room > 0 {
		keep := min(room, len(p))
		w.buf.Write(p[:keep])
		p = p[keep:]
	}
	if len(p) > 0 {
		w.truncated = true
	}
	return n, nil
}

func (w *CapWriter) String() string {
	if w.truncated {
		return w.buf.String() + truncatedMarker
	}
	return w.buf.String()
}
