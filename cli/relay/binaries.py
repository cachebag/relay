"""Where relayd and relay-agent live, and how `relay install` gets them.

Every command that starts one of the binaries finds it the same way:
`$RELAYD` or `$RELAY_AGENT` if set, then `<config dir>/bin/`, then PATH.
"""

import json
import os
import platform
import shutil
import stat
import sys
import tarfile
from collections.abc import Callable
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
    return f"{name}-{os_name}-{arch}" + (".exe" if os_name == "windows" else ".tar.gz")


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


def latest_tag(client: httpx.Client) -> str:
    """The tag the latest release redirect points at, e.g. v0.2.6."""
    r = client.get(f"{REPO}/releases/latest", follow_redirects=False)
    location = r.headers.get("location", "")
    if "/tag/" in location:
        return location.rsplit("/tag/", 1)[1].strip("/")
    r.raise_for_status()
    raise MissingBinary("couldn't find the latest release; is the repo public?")


def installed_versions() -> dict:
    try:
        return json.loads((install_dir() / "versions.json").read_text())
    except OSError, ValueError:
        return {}


def record_version(name: str, tag: str) -> None:
    versions = installed_versions()
    versions[name] = tag
    (install_dir() / "versions.json").write_text(json.dumps(versions, indent=2))


def download(
    name: str,
    version: str | None = None,
    client: httpx.Client | None = None,
    progress: Callable[[int, int | None], None] | None = None,
) -> tuple[Path, str, bool]:
    """Fetch one binary into the install dir.

    Returns (path, tag, downloaded). downloaded is False when that tag is
    already installed. progress(bytes_so_far, total_or_None) is called as
    chunks land."""
    os_name, arch = this_platform()
    if (os_name, arch) not in PUBLISHED:
        raise MissingBinary(
            f"no release build for {os_name}/{arch}; "
            f"build from source with go build ./cmd/{name}"
        )
    client = client or httpx.Client(timeout=120, follow_redirects=True)
    if version:
        tag = version if version.startswith("v") else "v" + version
    else:
        tag = latest_tag(client)
    dest = install_dir() / (name + (".exe" if os_name == "windows" else ""))
    if dest.exists() and installed_versions().get(name) == tag:
        return dest, tag, False

    url = f"{REPO}/releases/download/{tag}/{asset_name(name, os_name, arch)}"
    dest.parent.mkdir(parents=True, exist_ok=True)
    tmp = dest.with_suffix(dest.suffix + ".part")
    with client.stream("GET", url) as r:
        r.raise_for_status()
        length = r.headers.get("content-length")
        total = int(length) if length else None
        done = 0
        with open(tmp, "wb") as f:
            for chunk in r.iter_bytes():
                f.write(chunk)
                done += len(chunk)
                if progress:
                    progress(done, total)
    if os_name == "windows":
        tmp.chmod(tmp.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
        tmp.replace(dest)
    else:
        # Unpack beside dest and rename over it. Writing into a binary that is
        # running keeps its inode, and macOS then kills every new launch of it.
        new = dest.with_suffix(dest.suffix + ".new")
        with tarfile.open(tmp) as tar:
            src = tar.extractfile(tar.getmember(name))
            with open(new, "wb") as f:
                shutil.copyfileobj(src, f)
        new.chmod(new.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
        new.replace(dest)
        tmp.unlink()
    record_version(name, tag)
    return dest, tag, True
