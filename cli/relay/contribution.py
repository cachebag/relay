"""Manage the local relay-agent process used by ``relay contribute``."""

import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

from relay import binaries, profile

STARTUP_SECONDS = 10
STOP_SECONDS = 10


class ContributionError(RuntimeError):
    """A user-facing error while starting or managing the agent."""

class AgentExitedError(ContributionError):
    """relay-agent exited during startup; ``line`` is the last thing it printed."""

def __init__(self, line: str) -> None:
    super().__init__(f"relay-agent exited during startup: {line}")
    self.line = line


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
        handle = kernel.OpenProcess(0x00100000, False, pid)  # SYNCHRONIZE
        if not handle:
            return False
        try:
            return kernel.WaitForSingleObject(handle, 0) == 0x102  # WAIT_TIMEOUT
        finally:
            kernel.CloseHandle(handle)
    # If the agent is our own child, collect it once it has exited. Otherwise
    # it lingers as a zombie, which os.kill(pid, 0) still reports as alive,
    # and the dashboard would think a stopped agent is running forever.
    try:
        reaped, _ = os.waitpid(pid, os.WNOHANG)
        if reaped == pid:
            return False
    except ChildProcessError:
        pass  # not our child, for example started by an earlier CLI run
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _last_log_line(log_path: Path, start: int) -> str:
    with log_path.open("r", encoding="utf-8", errors="replace") as log:
        log.seek(start)
        lines = [line.strip() for line in log if line.strip()]
    return lines[-1] if lines else "no error output was written"


def _tracking_paths() -> tuple[Path, Path]:
    directory = profile.config_dir()
    return directory / "relay-agent.pid", directory / "relay-agent.state.json"


def _clear_tracking() -> None:
    pid_path, state_path = _tracking_paths()
    pid_path.unlink(missing_ok=True)
    state_path.unlink(missing_ok=True)


def status() -> dict | None:
    """Describe the running local agent, or clean up stale state and return None."""
    pid_path, state_path = _tracking_paths()
    try:
        pid = int(pid_path.read_text(encoding="utf-8").strip())
    except FileNotFoundError:
        return None
    except ValueError:
        _clear_tracking()
        return None
    except OSError as exc:
        raise ContributionError(f"could not read agent PID: {exc}") from exc

    if not _pid_alive(pid):
        _clear_tracking()
        return None

    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except OSError, ValueError:
        state = {}
    if not isinstance(state, dict) or state.get("pid") != pid:
        state = {}

    return {
        "pid": pid,
        "pool_id": state.get("pool_id"),
        "pool_name": state.get("pool_name"),
    }


def stop() -> dict | None:
    """Ask the tracked agent to shut down, waiting for its process to exit."""
    running = status()
    if running is None:
        return None

    pid = running["pid"]
    # SIGTERM would force-kill a Windows process. Ctrl+Break reaches the
    # os.Interrupt handler already registered by the Go agent instead.
    stop_signal = signal.CTRL_BREAK_EVENT if sys.platform == "win32" else signal.SIGTERM
    try:
        os.kill(pid, stop_signal)
    except OSError as exc:
        if not _pid_alive(pid):
            _clear_tracking()
            return running
        raise ContributionError(f"could not stop agent PID {pid}: {exc}") from exc

    deadline = time.monotonic() + STOP_SECONDS
    while time.monotonic() < deadline and _pid_alive(pid):
        time.sleep(0.1)

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

    # Use the shared resolver so RELAY_AGENT and newly installed binaries in
    # Relay's own bin folder work even before the user's PATH is refreshed.
    try:
        agent = str(binaries.find("relay-agent"))
    except binaries.MissingBinary as exc:
        raise ContributionError(str(exc)) from exc
    directory = profile.config_dir()
    directory.mkdir(parents=True, exist_ok=True)
    pid_path, state_path = _tracking_paths()
    log_path = directory / "relay-agent.log"

    already_running = status()
    if already_running is not None:
        raise ContributionError(f"already contributing (PID {already_running['pid']})")

    argv = [
        agent,
        "--scheduler",
        scheduler,
        "--pool-token",
        token,
        "--owner",
        owner,
    ]
    if sys.platform == "win32":
        options = {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP}
    else:
        options = {"start_new_session": True}

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
            raise ContributionError(f"could not launch relay-agent: {exc}") from exc

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

        deadline = time.monotonic() + STARTUP_SECONDS
        while time.monotonic() < deadline and process.poll() is None:
            time.sleep(0.1)
        exited = process.poll() is not None

    if exited:
        _clear_tracking()
        detail = _last_log_line(log_path, log_start)
        raise AgentExitedError(detail)
    return process.pid, log_path
