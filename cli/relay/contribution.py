"""Manage independently running relay-agent processes for saved pools."""

import json
import os
import re
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


def _tracking_paths(pool_id: str) -> tuple[Path, Path]:
    """Keep each pool's PID and state separate from every other pool."""
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", pool_id):
        raise ContributionError(f"invalid pool ID: {pool_id!r}")
    directory = profile.config_dir()
    prefix = f"relay-agent.{pool_id}"
    return directory / f"{prefix}.pid", directory / f"{prefix}.state.json"


def _legacy_tracking_paths() -> tuple[Path, Path]:
    """Older CLI versions kept a single process in these two files."""
    directory = profile.config_dir()
    return directory / "relay-agent.pid", directory / "relay-agent.state.json"


def _clear_tracking(paths: tuple[Path, Path]) -> None:
    pid_path, state_path = paths
    pid_path.unlink(missing_ok=True)
    state_path.unlink(missing_ok=True)


def _read_running(paths: tuple[Path, Path], pool_id: str | None) -> dict | None:
    """Read one tracked process, cleaning up only that process if it is stale."""
    pid_path, state_path = paths
    try:
        pid = int(pid_path.read_text(encoding="utf-8").strip())
    except FileNotFoundError:
        return None
    except ValueError:
        _clear_tracking(paths)
        return None
    except OSError as exc:
        raise ContributionError(f"could not read agent PID: {exc}") from exc

    if not _pid_alive(pid):
        _clear_tracking(paths)
        return None

    try:
        state = json.loads(state_path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        state = {}
    if not isinstance(state, dict) or state.get("pid") != pid:
        state = {}

    return {
        "pid": pid,
        "pool_id": state.get("pool_id") or pool_id,
        "pool_name": state.get("pool_name"),
        "scheduler": state.get("scheduler"),
    }


def _running_with_paths() -> list[tuple[dict, tuple[Path, Path]]]:
    """Find every tracked agent, retaining its files for targeted shutdown."""
    directory = profile.config_dir()
    running = []
    known_pids = set()
    for pid_path in sorted(directory.glob("relay-agent.*.pid")):
        pool_id = pid_path.name[len("relay-agent.") : -len(".pid")]
        paths = (pid_path, directory / f"relay-agent.{pool_id}.state.json")
        state = _read_running(paths, pool_id)
        if state is not None:
            running.append((state, paths))
            known_pids.add(state["pid"])

    old_paths = _legacy_tracking_paths()
    old = _read_running(old_paths, None)
    if old is not None and old["pid"] not in known_pids:
        running.append((old, old_paths))
    return running


def statuses() -> list[dict]:
    """List all live agents, including agents started by an older CLI."""
    return [state for state, _ in _running_with_paths()]


def _find_running(pool_id: str) -> tuple[dict | None, tuple[Path, Path]]:
    paths = _tracking_paths(pool_id)
    running = _read_running(paths, pool_id)
    if running is not None:
        return running, paths

    # Do not start a duplicate when an agent from a previous CLI version is
    # still running for this pool. It can also be stopped normally.
    old_paths = _legacy_tracking_paths()
    old = _read_running(old_paths, None)
    if old is not None and old["pool_id"] == pool_id:
        return old, old_paths
    return None, paths


def status(pool_id: str | None = None) -> dict | None:
    """Describe the current (or selected) pool's local agent."""
    if pool_id is None:
        pool_id = profile.load().get("pool_id")
    if not pool_id:
        return None
    running, _ = _find_running(pool_id)
    return running


def _stop_running(running: dict, paths: tuple[Path, Path]) -> dict:
    """Shut down one known agent without altering any other tracking files."""
    pid = running["pid"]
    # SIGTERM would force-kill a Windows process. Ctrl+Break reaches the
    # os.Interrupt handler already registered by the Go agent instead.
    stop_signal = signal.CTRL_BREAK_EVENT if sys.platform == "win32" else signal.SIGTERM
    try:
        os.kill(pid, stop_signal)
    except OSError as exc:
        if not _pid_alive(pid):
            _clear_tracking(paths)
            return running
        raise ContributionError(f"could not stop agent PID {pid}: {exc}") from exc

    deadline = time.monotonic() + STOP_SECONDS
    while time.monotonic() < deadline and _pid_alive(pid):
        time.sleep(0.1)

    if _pid_alive(pid):
        raise ContributionError(
            f"sent stop signal to PID {pid}, but it is still running; "
            "check the pool's relay-agent log and try status again"
        )

    _clear_tracking(paths)
    return running


def stop(pool_id: str | None = None) -> dict | None:
    """Stop only the current (or selected) pool's agent."""
    if pool_id is None:
        pool_id = profile.load().get("pool_id")
    if not pool_id:
        return None
    running, paths = _find_running(pool_id)
    return _stop_running(running, paths) if running is not None else None


def stop_all() -> list[dict]:
    """Stop all tracked agents, continuing even if one fails to shut down."""
    stopped = []
    errors = []
    for running, paths in _running_with_paths():
        try:
            stopped.append(_stop_running(running, paths))
        except ContributionError as exc:
            errors.append(str(exc))
    if errors:
        raise ContributionError("could not stop every agent: " + "; ".join(errors))
    return stopped


def start(prof: dict, scheduler: str) -> tuple[int, Path]:
    """Start one pool's agent without disturbing agents in other pools."""
    token = prof.get("pool_token")
    owner = prof.get("name")
    if not token or not owner:
        raise ContributionError("not logged in, run relay login first")
    pool_id = prof.get("pool_id")
    if not pool_id:
        raise ContributionError("saved login is missing a pool ID")

    # Use the shared resolver so RELAY_AGENT and newly installed binaries in
    # Relay's own bin folder work even before the user's PATH is refreshed.
    try:
        agent = str(binaries.find("relay-agent"))
    except binaries.MissingBinary as exc:
        raise ContributionError(str(exc)) from exc
    directory = profile.config_dir()
    directory.mkdir(parents=True, exist_ok=True)
    pid_path, state_path = _tracking_paths(pool_id)
    log_path = directory / f"relay-agent.{pool_id}.log"

    already_running = status(pool_id)
    if already_running is not None:
        raise ContributionError(
            f"already contributing to this pool (PID {already_running['pid']})"
        )

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
        _clear_tracking((pid_path, state_path))
        detail = _last_log_line(log_path, log_start)
        raise AgentExitedError(detail)
    return process.pid, log_path
