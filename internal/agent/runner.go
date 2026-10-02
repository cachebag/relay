package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/cachebag/relay/internal/store"
)

// Caps how much of stdout and stderr is kept per job.
const maxOutputBytes = 1 << 20

// Exit codes reported for when the container never produces one of its own.
const (
	exitTimeout = 124 // job exceeded its timeout and was killed
	exitNoStart = 125 // docker itself could not be started
)

// Result is what a finished job reports back to the scheduler.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Runner executes a job and reports how it went. A non-nil error means the
// job could not be run at all; a job that ran and failed is a Result with a
// non-zero ExitCode.
type Runner interface {
	Run(ctx context.Context, j store.Job) (Result, error)
}

// DockerRunner runs every job in its own throwaway container, so job commands
// never execute directly on the host.
type DockerRunner struct {
	// Image is the container image every job runs in.
	Image string
	// Network is the docker network mode. Empty means "none": jobs get no
	// network access unless the operator opts in.
	Network string

	// command builds the docker process. Tests replace it; it defaults to
	// exec.CommandContext on the "docker" binary.
	command func(ctx context.Context, args ...string) *exec.Cmd
}

// NewDockerRunner creates a new DockerRunner with the specified image and network mode.
func NewDockerRunner(image string, network string) *DockerRunner {
	return &DockerRunner{
		Image:   image,
		Network: network,
	}
}

func (d *DockerRunner) cmd(ctx context.Context, args ...string) *exec.Cmd {
	if d.command != nil {
		return d.command(ctx, args...)
	}
	return exec.CommandContext(ctx, "docker", args...)
}

// containerName is derived from the job ID so a stuck container can be
// found and removed by name if the docker CLI process is killed.
func containerName(j store.Job) string {
	return "relay-job-" + j.ID
}

// runArgs builds the `docker run` arguments for a job. The command is passed
// as separate arguments, never through a shell.
func (d *DockerRunner) runArgs(j store.Job) []string {
	network := d.Network
	if network == "" {
		network = "none"
	}
	args := []string{
		"run", "--rm",
		"--name", containerName(j),
		"--network", network,
		d.Image,
	}
	return append(args, j.Command...)
}

// Check verifies that the docker CLI is installed and its daemon is reachable.
func (d *DockerRunner) Check(ctx context.Context) error {
	out, err := d.cmd(ctx, "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker is not available: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// Run executes the specified job using Docker and captures its output.
// It returns the job's result and any error encountered.
func (d *DockerRunner) Run(ctx context.Context, j store.Job) (Result, error) {
	if len(j.Command) == 0 {
		return Result{ExitCode: 1, Stderr: "relay: job has no command"}, nil
	}

	runCtx := ctx

	// Prepare buffers to capture stdout and stderr, capped to a maximum size.
	stdout := &cappedBuffer{limit: maxOutputBytes}
	stderr := &cappedBuffer{limit: maxOutputBytes}

	// Create the docker command with the prepared arguments.
	c := d.cmd(runCtx, d.runArgs(j)...)
	c.Stdout = stdout
	c.Stderr = stderr

	// Run the docker command and capture its output.
	err := c.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	// Check if the context was canceled or timed out. If so, clean up the container and return a timeout result.
	if runCtx.Err() != nil {
		// Killing the docker CLI does not stop the container, so remove it
		// explicitly. This uses a fresh context since runCtx is already done.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = d.cmd(cleanupCtx, "rm", "-f", containerName(j)).Run()

		// Agent shutdown is not a job failure: leave the job unreported.
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		res.ExitCode = exitTimeout
		return res, nil
	}

	// Determine the exit code based on the error returned by the docker command.
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		// The docker binary could not be started at all.
		res.ExitCode = exitNoStart
		res.Stderr += fmt.Sprintf("relay: could not run docker: %v", err)
	}
	return res, nil
}

// cappedBuffer keeps the first limit bytes written to it and silently drops
// the rest, so a chatty job cannot exhaust the agent's memory.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

// Write appends data to the buffer, keeping only the first limit bytes. It always reports
// the full length of the input to avoid failing the docker process.
func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		c.truncated = true
	} else {
		c.buf.Write(p)
	}
	// Report the full length so the writer never fails the docker process.
	return len(p), nil
}

// String returns the contents of the buffer as a string. If the buffer was truncated,
// it appends a notice indicating that the output was truncated.
func (c *cappedBuffer) String() string {
	if c.truncated {
		return c.buf.String() + "\n[output truncated]"
	}
	return c.buf.String()
}
