"""Where relayd and relay-agent live, and how `relay install` gets them.

Every command that starts one of the binaries finds it the same way:
`$RELAYD` or `$RELAY_AGENT` if set, then `<config dir>/bin/`, then PATH.
"""

import os
import platform
import shutil
import stat
import sys
from pathlib import Path

import httpx

from relay import profile

REPO = "https://github.com/cachebag/relay"
NAMES = ("relayd", "relay-agent")
ENV = {"relayd": "RELAYD", "relay-agent": "RELAY_AGENT"}
# What the release workflow publishes.
PUBLISHED = {
    ("linux", "amd64"),
    ("darwin", "arm64"),
    ("darwin", "amd64"),
    ("windows", "amd64"),
}


class MissingBinary(Exception):
    """A binary could not be found. str(exc) says what to do."""


def install_dir() -> Path:
    return profile.config_dir() / "bin"


def this_platform() -> tuple[str, str]:
    """(os, arch) the way the release assets are named."""
    system = platform.system().lower()
    os_name = {"darwin": "darwin", "linux": "linux", "windows": "windows"}.get(
        system, system
    )
    machine = platform.machine().lower()
    arch = {
        "x86_64": "amd64",
        "amd64": "amd64",
        "arm64": "arm64",
        "aarch64": "arm64",
    }.get(machine, machine)
    return os_name, arch


def asset_name(name: str, os_name: str, arch: str) -> str:
    return f"{name}-{os_name}-{arch}" + (".exe" if os_name == "windows" else "")


def find(name: str) -> Path:
    """The path to relayd or relay-agent, or MissingBinary with a hint."""
    env = os.environ.get(ENV[name])
    if env:
        return Path(env)
    exe = name + (".exe" if sys.platform == "win32" else "")
    local = install_dir() / exe
    if local.exists():
        return local
    on_path = shutil.which(name)
    if on_path:
        return Path(on_path)
    raise MissingBinary(f"{name} not found; run relay install first")


def download(
    name: str, version: str | None = None, client: httpx.Client | None = None
) -> tuple[Path, str]:
    """Fetch one binary into the install dir. Returns (path, version tag)."""
    os_name, arch = this_platform()
    if (os_name, arch) not in PUBLISHED:
        raise MissingBinary(
            f"no release build for {os_name}/{arch}; build from source with go build ./cmd/{name}"
        )
    client = client or httpx.Client(timeout=120, follow_redirects=True)
    asset = asset_name(name, os_name, arch)
    if version:
        tag = version if version.startswith("v") else "v" + version
        url = f"{REPO}/releases/download/{tag}/{asset}"
    else:
        url = f"{REPO}/releases/latest/download/{asset}"
    r = client.get(url)
    r.raise_for_status()
    # After the redirects, the final URL names the tag that was served.
    parts = str(r.url).split("/")
    tag = (
        parts[parts.index("download") + 1]
        if "download" in parts
        else version or "latest"
    )

    dest = install_dir() / (name + (".exe" if os_name == "windows" else ""))
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".part")
    tmp.write_bytes(r.content)
    tmp.chmod(tmp.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
    tmp.replace(dest)
    return dest, tag
