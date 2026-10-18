"""The saved login profile: scheduler, pool, join token, and user name.

This is the only file the CLI keeps outside its install. It holds the join
token, so it is written readable by the owner only on Linux and macOS.
"""

import json
import os
import sys
from pathlib import Path

FILENAME = "cli.json"


def config_dir() -> Path:
    if sys.platform == "linux":
        base = os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config"
        return Path(base) / "relay"
    if sys.platform == "darwin":
        return Path.home() / "Library" / "Application Support" / "relay"
    if sys.platform == "win32":
        base = os.environ.get("APPDATA") or Path.home() / "AppData" / "Roaming"
        return Path(base) / "relay"
    # Error message for unsupported platforms
    raise RuntimeError(f"Unsupported platform: {sys.platform}")


def path() -> Path:
    return config_dir() / FILENAME


def load() -> dict:
    """Return the saved profile, or an empty dict if not logged in."""
    try:
        data = json.loads(path().read_text())
    except OSError, ValueError:
        return {}
    return data if isinstance(data, dict) else {}


def save(profile: dict) -> None:
    p = path()
    p.parent.mkdir(parents=True, exist_ok=True)
    # Create or overwrite the profile file with owner only rw permissions
    fd = os.open(p, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(profile, f, indent=2)
    if sys.platform != "win32":
        p.chmod(0o600)


def delete() -> None:
    path().unlink(missing_ok=True)
