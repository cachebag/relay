"""Run relayd or relay-agent in the background and find them again later.

Each one gets <config dir>/<name>.log for its output and <name>.pid for its
process ID. Nothing here is specific to either binary.
"""

import json
import os
import signal
import subprocess
import sys
import time
from collections.abc import Callable
from pathlib import Path

from relay import profile


def log_path(name: str) -> Path:
    return profile.config_dir() / f"{name}.log"


def pid_path(name: str) -> Path:
    return profile.config_dir() / f"{name}.pid"


def argv_path(name: str) -> Path:
    return profile.config_dir() / f"{name}.argv.json"


def argv(name: str) -> list[str] | None:
    """What the process was last started with, if those flags were saved."""
    try:
        saved = json.loads(argv_path(name).read_text())
    except (OSError, ValueError):
        return None
    return saved if isinstance(saved, list) else None


def _windows_pid_alive(pid: int) -> bool:
    """Check a Windows process without using os.kill(pid, 0).

    On Windows, os.kill(pid, 0) can *terminate* a process. Instead, open a
    synchronization handle and ask whether the process has exited.
    """
    import ctypes
    from ctypes import wintypes

    synchronize = 0x00100000
    wait_object_0 = 0x00000000
    wait_timeout = 0x00000102
    error_access_denied = 5
    error_invalid_parameter = 87

    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    open_process = kernel32.OpenProcess
    open_process.argtypes = (wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)
    open_process.restype = wintypes.HANDLE
    wait_for_single_object = kernel32.WaitForSingleObject
    wait_for_single_object.argtypes = (wintypes.HANDLE, wintypes.DWORD)
    wait_for_single_object.restype = wintypes.DWORD
    close_handle = kernel32.CloseHandle
    close_handle.argtypes = (wintypes.HANDLE,)
    close_handle.restype = wintypes.BOOL

    handle = open_process(synchronize, False, pid)
    if not handle:
        error = ctypes.get_last_error()
        if error == error_invalid_parameter:
            return False  # No such PID.
        if error == error_access_denied:
            return True  # Exists, but we cannot inspect it.
        raise ctypes.WinError(error)

    try:
        result = wait_for_single_object(handle, 0)
        if result == wait_object_0:
            return False  # Process has exited.
        if result == wait_timeout:
            return True  # Still running.
        raise ctypes.WinError(ctypes.get_last_error())
    finally:
        close_handle(handle)


def _pid_alive(pid: int) -> bool:
    """Return whether a PID exists, without accidentally stopping it."""
    if pid <= 0:
        return False
    if sys.platform == "win32":
        return _windows_pid_alive(pid)
    try:
        os.kill(pid, 0)  # Safe existence check on macOS and Linux.
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def running(name: str) -> int | None:
    """Return the saved PID if alive; clear stale or invalid PID files."""
    path = pid_path(name)
    try:
        pid = int(path.read_text().strip())
    except (OSError, ValueError):
        return None
    if _pid_alive(pid):
        return pid
    path.unlink(missing_ok=True)
    return None


def start(name: str, argv: list[str], env: dict | None = None) -> int:
    """Start argv detached, output to the log, cwd in the config dir."""
    profile.config_dir().mkdir(parents=True, exist_ok=True)
    kwargs: dict = {}
    if sys.platform == "win32":
        kwargs["creationflags"] = (
            subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.DETACHED_PROCESS
        )
    else:
        kwargs["start_new_session"] = True

    # The child inherits the open log; the parent can close its copy right away.
    with open(log_path(name), "ab") as log:
        proc = subprocess.Popen(
            argv,
            cwd=profile.config_dir(),
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=log,
            stderr=subprocess.STDOUT,
            **kwargs,
        )
    pid_path(name).write_text(str(proc.pid))
    # relay update restarts the process with the same flags.
    argv_path(name).write_text(json.dumps(argv))
    return proc.pid


def stop(name: str, timeout: float = 5.0) -> bool:
    """Stop the saved process and verify it exited before clearing its PID.

    Return False if no tracked process is running, True once it has exited.
    Raise an error if shutdown fails, preserving the PID file so that the CLI
    can try again. On Windows, SIGTERM uses TerminateProcess; CTRL_BREAK_EVENT
    is unsuitable for the detached processes started here.
    """
    pid = running(name)
    if pid is None:
        return False

    try:
        # Windows: TerminateProcess (SIGTERM through os.kill).
        # macOS/Linux: normal SIGTERM, allowing the process to handle it.
        os.kill(pid, signal.SIGTERM)
    except OSError:
        # The process might have exited between running() and os.kill().
        if _pid_alive(pid):
            raise

    deadline = time.monotonic() + max(timeout, 0.0)
    while _pid_alive(pid):
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError(
                f"{name} (pid {pid}) did not exit within {timeout:g} seconds; "
                f"PID file retained at {pid_path(name)}"
            )
        time.sleep(min(0.1, remaining))

    pid_path(name).unlink(missing_ok=True)
    return True


def wait_for(
    name: str,
    patterns: list[str],
    timeout: float,
    start_at: int = 0,
    is_running: Callable[[], bool] | None = None,
) -> dict:
    """Watch the log from offset start_at until every pattern has a line, or
    the process dies, or timeout. Returns {pattern: matching line}."""
    found: dict[str, str] = {}
    deadline = time.monotonic() + timeout
    while True:
        try:
            with log_path(name).open("rb") as log:
                log.seek(0, os.SEEK_END)
                log.seek(start_at if start_at <= log.tell() else 0)
                text = log.read().decode(errors="replace")
        except OSError:
            text = ""
        for line in text.splitlines():
            for pat in patterns:
                if pat not in found and pat in line:
                    found[pat] = line.strip()
        # One pass always runs, so a timeout of 0 means "read what's there".
        if len(found) == len(patterns):
            break
        alive = is_running() if is_running is not None else running(name) is not None
        if not alive or time.monotonic() >= deadline:
            break
        time.sleep(0.2)
    return found


def tail(name: str, lines: int = 10) -> str:
    try:
        return "\n".join(
            log_path(name).read_text(errors="replace").splitlines()[-lines:]
        )
    except OSError:
        return ""
