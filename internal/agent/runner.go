package agent

import (
	"context"
	"errors"

	"github.com/cachebag/relay/internal/store"
)

// Result is what a finished job reports back to the scheduler.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type Runner interface {
	Run(ctx context.Context, j store.Job) (Result, error)
}

// DockerRunner runs every job in its own throwaway container, so job commands
// never execute directly on the host.
type DockerRunner struct {
}

func NewDockerRunner(image string, network string) *DockerRunner {
	return &DockerRunner{}
}

func (d *DockerRunner) Check(ctx context.Context) error {
	return errors.New("hmm")
}

func (d *DockerRunner) Run(ctx context.Context, j store.Job) (Result, error) {
	return Result{}, errors.New("idk")
}
