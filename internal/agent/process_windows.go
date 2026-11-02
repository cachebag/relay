//go:build windows

package agent

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func configureJobProcess(_ *exec.Cmd) {
	// taskkill /T uses the Windows parent/child process relationship.
}

func killJobProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	// taskkill /T /F terminates the process and the children it started.
	// Give the cleanup command its own context because the job's has expired.
	cleanupCtx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Second,
	)
	defer cancel()

	kill := exec.CommandContext(
		cleanupCtx,
		"taskkill",
		"/PID",
		strconv.Itoa(cmd.Process.Pid),
		"/T",
		"/F",
	)

	kill.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	if err := kill.Run(); err != nil {
		return cmd.Process.Kill()
	}

	return nil
}
