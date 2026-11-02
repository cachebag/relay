//go:build linux || darwin

package agent

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureJobProcess(cmd *exec.Cmd) {
	// Give the command a fresh process group so its descendants can be
	// terminated without killing the relay agent's own process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func killJobProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	// A negative PID addresses the entire process group.
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)

	if errors.Is(err, syscall.ESRCH) {
		return nil
	}

	if err != nil {
		return cmd.Process.Kill()
	}

	return nil
}
