"""Manage the local relay-agent process used by relay contribute."""

import json
import os
import platform
import shutil
import subprocess
import sys
import time
from pathlib import Path

from relay import profile

STARTUP_SECONDS = 10


class ContributionError(RuntimeError):
    """A user-facing error while starting or managing the agent."""

# Looks for the relay-agent executable installed on the user's system.
# Supports both the standard name and platform-specific release filenames.
def find_agent() -> str:
    """Find an installed agent, including the names used by release assets."""
    system = {"win32": "windows", "darwin": "darwin", "linux": "linux"}.get(
        sys.platform, sys.platform
    )
    machine = platform.machine().lower()
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(machine, machine)
    suffix = ".exe" if sys.platform == "win32" else ""

    for name in (f"relay-agent{suffix}", f"relay-agent-{system}-{arch}{suffix}"):
        found = shutil.which(name)
        if found:
            return found

    raise ContributionError(
        "relay-agent was not found on PATH; install it and try again"
    )

# Checks whether a process is still running using its PID.
# Windows requires a different method since os.kill(pid, 0) is not safe there.
def _pid_alive(pid: int) -> bool:
    """Check a PID without sending it a termination signal on Windows."""
    if pid <= 0:
        return False

    if sys.platform == "win32":
        import ctypes
        from ctypes import wintypes

        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.OpenProcess.argtypes = (wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)
        kernel.OpenProcess.restype = wintypes.HANDLE
        kernel.WaitForSingleObject.argtypes = (wintypes.HANDLE, wintypes.DWORD)
        kernel.WaitForSingleObject.restype = wintypes.DWORD
        kernel.CloseHandle.argtypes = (wintypes.HANDLE,)

        handle = kernel.OpenProcess(0x00100000, False, pid)
        if not handle:
            return False

        try:
            return kernel.WaitForSingleObject(handle, 0) == 0x102
        finally:
            kernel.CloseHandle(handle)

    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True

    return True

# Retrieves the last line written during startup to help explain why the agent failed to start.
def _last_log_line(log_path: Path, start: int) -> str:
    with log_path.open("r", encoding="utf-8", errors="replace") as log:
        log.seek(start)
        lines = [line.strip() for line in log if line.strip()]

    return lines[-1] if lines else "no error output was written"


def start(prof: dict, scheduler: str) -> tuple[int, Path]:
    """Start the saved pool's agent and check its first 10 seconds."""
    token = prof.get("pool_token")
    owner = prof.get("name")

    if not token or not owner:
        raise ContributionError("not logged in, run relay login first")

    # Find the agent executable and prepare the files used to track
    # its process information and log output.
    agent = find_agent()
    directory = profile.config_dir()
    directory.mkdir(parents=True, exist_ok=True)

    pid_path = directory / "relay-agent.pid"
    state_path = directory / "relay-agent.state.json"
    log_path = directory / "relay-agent.log"

    # Check whether an agent is already running before starting another.
    # Remove outdated PID information if the previous process has stopped.
    if pid_path.exists():
        try:
            old_pid = int(pid_path.read_text().strip())
        except (OSError, ValueError):
            old_pid = -1

        if _pid_alive(old_pid):
            raise ContributionError(f"already contributing (PID {old_pid})")

        pid_path.unlink(missing_ok=True)
        state_path.unlink(missing_ok=True)

    # Build the command using the scheduler, pool token, and username stored in the current login profile.
    argv = [
        agent,
        "--scheduler",
        scheduler,
        "--pool-token",
        token,
        "--owner",
        owner,
    ]

    # Start the agent in a separate process group on Windows or a new
    # session on Unix so it can continue running after the CLI exits.
    if sys.platform == "win32":
        options = {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP}
    else:
        options = {"start_new_session": True}

    # Redirect the agent's output to a log file instead of the terminal.
    # This allows the agent to continue running in the background.
    with log_path.open("a+", encoding="utf-8") as log:
        log.seek(0, 2)
        log_start = log.tell()

        try:
            process = subprocess.Popen(
                argv,
                stdin=subprocess.DEVNULL,
                stdout=log,
                stderr=subprocess.STDOUT,
                close_fds=True,
                **options,
            )
        except OSError as exc:
            raise ContributionError(
                f"could not launch relay-agent: {exc}"
            ) from exc

        # Save the process ID and pool information so future commands can identify and manage the running agent.
        pid_path.write_text(f"{process.pid}\n", encoding="utf-8")

        state_path.write_text(
            json.dumps(
                {
                    "pid": process.pid,
                    "pool_id": prof.get("pool_id"),
                    "pool_name": prof.get("pool_name"),
                    "scheduler": scheduler,
                }
            ),
            encoding="utf-8",
        )

        # Monitor the agent for its first 10 seconds to catch startup failures
        # before reporting that it started successfully.
        deadline = time.monotonic() + STARTUP_SECONDS

        while time.monotonic() < deadline and process.poll() is None:
            time.sleep(0.1)

        exited = process.poll() is not None

    # Clean up the saved process information if startup failed
    # and display the last log message to help identify the problem.
    if exited:
        pid_path.unlink(missing_ok=True)
        state_path.unlink(missing_ok=True)

        detail = _last_log_line(log_path, log_start)

        raise ContributionError(
            f"relay-agent exited during startup: {detail}"
        )

    return process.pid, log_path