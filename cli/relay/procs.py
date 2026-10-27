"""Run relayd or relay-agent in the background and find them again later.

Each one gets <config dir>/<name>.log for its output and <name>.pid for its
process ID. Nothing here is specific to either binary.
"""

import os
import signal
import subprocess
import sys
import time
from pathlib import Path

from relay import profile


def log_path(name: str) -> Path:
    return profile.config_dir() / f"{name}.log"


def pid_path(name: str) -> Path:
    return profile.config_dir() / f"{name}.pid"


def running(name: str) -> int | None:
    """The PID if the process from the pid file is alive, else None."""
    try:
        pid = int(pid_path(name).read_text().strip())
    except OSError, ValueError:
        return None
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        pid_path(name).unlink(missing_ok=True)
        return None
    except PermissionError:
        pass
    return pid


def start(name: str, argv: list[str], env: dict | None = None) -> int:
    """Start argv detached, output to the log, cwd in the config dir. Returns the PID."""
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
    return proc.pid


def stop(name: str, timeout: float = 5.0) -> bool:
    """SIGTERM the process from the pid file. True if there was one."""
    pid = running(name)
    if pid is None:
        return False
    try:
        if sys.platform == "win32":
            os.kill(pid, signal.CTRL_BREAK_EVENT)
        else:
            os.kill(pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            break
        time.sleep(0.1)
    pid_path(name).unlink(missing_ok=True)
    return True


def wait_for(name: str, patterns: list[str], timeout: float, start_at: int = 0) -> dict:
    """Watch the log from offset start_at until every pattern has a line, or
    the process dies, or timeout. Returns {pattern: matching line}."""
    found: dict[str, str] = {}
    deadline = time.monotonic() + timeout
    while True:
        try:
            text = log_path(name).read_bytes()[start_at:].decode(errors="replace")
        except OSError:
            text = ""
        for line in text.splitlines():
            for pat in patterns:
                if pat not in found and pat in line:
                    found[pat] = line.strip()
        # One pass always runs, so a timeout of 0 means "read what's there".
        if len(found) == len(patterns) or running(name) is None:
            break
        if time.monotonic() >= deadline:
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
