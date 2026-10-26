"""Manage the local relay-agent process used by relay contribute."""

import json
import os
import platform
import shutil
import signal
import subprocess
import sys
import time
from pathlib import Path

from relay import profile

STARTUP_SECONDS = 10
STOP_SECONDS = 10


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


# Returns the locations of the PID file and saved pool information.
def _tracking_paths() -> tuple[Path, Path]:
    directory = profile.config_dir()

    return directory / "relay-agent.pid", directory / "relay-agent.state.json"


# Removes the PID and pool information after the agent stops.
def _clear_tracking() -> None:
    pid_path, state_path = _tracking_paths()

    pid_path.unlink(missing_ok=True)
    state_path.unlink(missing_ok=True)


def status() -> dict | None:
    """Return the running agent information or None if it isn't running."""

    pid_path, state_path = _tracking_paths()

    # Read the saved PID to determine which process belongs to the agent.
    try:
        pid = int(pid_path.read_text(encoding="utf-8").strip())
    except FileNotFoundError:
        return None
    except ValueError:
        _clear_tracking()
        return None
    except OSError as exc:
        raise ContributionError(f"could not read agent PID: {exc}") from exc

    # If the process no longer exists, remove its outdated tracking files.
    if not _pid_alive(pid):
        _clear_tracking()
        return None

    # Load the pool information saved when the agent was started.
    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        state = {}

    if not isinstance(state, dict) or state.get("pid") != pid:
        state = {}

    return {
        "pid": pid,
        "pool_id": state.get("pool_id"),
        "pool_name": state.get("pool_name"),
    }


def stop() -> dict | None:
    """Stop the local agent and wait for its process to exit."""

    running = status()

    if running is None:
        return None

    pid = running["pid"]

    # Windows uses Ctrl+Break to trigger the Go agent's interrupt handler.
    # Linux and macOS use SIGTERM for graceful shutdown.
    stop_signal = (
        signal.CTRL_BREAK_EVENT if sys.platform == "win32" else signal.SIGTERM
    )

    try:
        os.kill(pid, stop_signal)
    except OSError as exc:
        if not _pid_alive(pid):
            _clear_tracking()
            return running

        raise ContributionError(
            f"could not stop agent PID {pid}: {exc}"
        ) from exc

    # Wait up to 10 seconds for the agent to finish shutting down.
    deadline = time.monotonic() + STOP_SECONDS

    while time.monotonic() < deadline and _pid_alive(pid):
        time.sleep(0.1)

    # Keep the PID information if the agent is still shutting down.
    if _pid_alive(pid):
        raise ContributionError(
            f"sent stop signal to PID {pid}, but it is still running; "
            "check relay-agent.log and try status again"
        )

    _clear_tracking()
    return running


def start(prof: dict, scheduler: str) -> tuple[int, Path]:
    """Start the saved pool's agent and verify it survives the first 10 seconds."""

    token = prof.get("pool_token")
    owner = prof.get("name")

    if not token or not owner:
        raise ContributionError("not logged in, run relay login first")

    # Find the agent executable and prepare the files used to track its process information and log output.
    agent = find_agent()
    directory = profile.config_dir()
    directory.mkdir(parents=True, exist_ok=True)

    pid_path, state_path = _tracking_paths()
    log_path = directory / "relay-agent.log"

    # Check whether an agent is already running before starting another.
    # The status function also removes outdated PID information.
    already_running = status()

    if already_running is not None:
        raise ContributionError(
            f"already contributing (PID {already_running['pid']})"
        )

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

    # Start the agent in a separate process group on Windows or a new session on Unix so it can continue running after the CLI exits.
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

        # Monitor the agent for its first 10 seconds to catch startup failures before reporting that it started successfully.
        deadline = time.monotonic() + STARTUP_SECONDS

        while time.monotonic() < deadline and process.poll() is None:
            time.sleep(0.1)

        exited = process.poll() is not None

    # Clean up the saved process information if startup failed and display the last log message to help identify the problem.
    if exited:
        _clear_tracking()
        detail = _last_log_line(log_path, log_start)

        raise ContributionError(
            f"relay-agent exited during startup: {detail}"
        )

    return process.pid, log_path