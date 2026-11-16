"""relay update: move this machine's relayd and relay-agent to a newer
release and restart whatever was running on the old one."""

import argparse
import os
import sqlite3
import sys
import time
from contextlib import closing
from importlib.metadata import version
from pathlib import Path

import httpx

from relay import binaries, contribution, procs, profile, services
from relay.commands import scheduler

RELAYD = "relayd"
AGENT = "relay-agent"
HEALTH_SECONDS = 20


def register(sub) -> None:
    p = sub.add_parser(
        "update",
        help="update relayd and relay-agent and restart what is running",
    )
    p.add_argument(
        "--version", help="release tag, for example v0.3.2 (default: latest)"
    )
    p.add_argument(
        "--check",
        action="store_true",
        help="only say what would change",
    )
    p.set_defaults(func=run)


def numbers(text: str) -> tuple[int, ...]:
    try:
        return tuple(int(part) for part in text.lstrip("v").split("."))
    except ValueError:
        return ()


def cli_note(tag: str) -> None:
    """The CLI is a separate install, so say when it is behind the binaries."""
    mine = version("relay")
    if numbers(mine) and numbers(mine) < numbers(tag):
        print(f"this CLI is {mine}; update it with: uv tool upgrade relay")


def backup_db(tag: str) -> Path | None:
    """Copy relay.db aside before a newer relayd migrates it. relayd must be
    stopped. None when this machine has no database."""
    db = profile.config_dir() / "relay.db"
    if not db.exists():
        return None
    dest = profile.config_dir() / "backups" / f"relay-before-{tag}.db"
    dest.parent.mkdir(parents=True, exist_ok=True)
    dest.unlink(missing_ok=True)
    with closing(sqlite3.connect(db)) as src, closing(sqlite3.connect(dest)) as dst:
        src.backup(dst)
    if sys.platform != "win32":
        # It holds the join tokens, like relay.db itself.
        os.chmod(dest, 0o600)
    return dest


def healthy(address: str) -> bool:
    deadline = time.monotonic() + HEALTH_SECONDS
    while True:
        try:
            if httpx.get(f"{address}/healthz", timeout=2).is_success:
                return True
        except httpx.HTTPError:
            pass
        if time.monotonic() >= deadline:
            return False
        time.sleep(0.5)


def stop_relayd(service: bool) -> None:
    if service:
        services.stop()
    else:
        procs.stop(RELAYD)


def start_relayd(service: bool, flags: argparse.Namespace | None) -> bool:
    """Start relayd the way it was running and wait for it to answer."""
    if service:
        flags = argparse.Namespace(service=True, public=None, port=None, name=None)
    try:
        scheduler.run_start(flags)
    except SystemExit as exc:
        print(exc, file=sys.stderr)
        return False
    return healthy(profile.server().get("address") or "http://localhost:8080")


def update_relayd(dest: Path, tag: str, old: str | None) -> None:
    try:
        service = services.installed()
        if service:
            was_running = services.state() in {"active", "activating", "reloading"}
        else:
            was_running = procs.running(RELAYD) is not None
        flags = None
        if was_running and not service:
            flags = scheduler.running_flags()
        if was_running:
            stop_relayd(service)
    except services.ServiceError as exc:
        raise SystemExit(f"relay update: {exc}; relayd was not changed") from exc

    backup = backup_db(tag)
    binaries.commit(dest, RELAYD, tag, keep_old=True)
    if not was_running:
        print(f"relayd updated to {tag}; it was not running")
        return
    if start_relayd(service, flags):
        print(f"relayd updated to {tag}")
        return

    # The new relayd did not come up, most likely on a migration. Put back
    # the binary and the database it was running on.
    was = old or "the version before"
    print(f"relayd {tag} did not start; going back to {was}", file=sys.stderr)
    log = procs.tail(RELAYD)
    restored = False
    try:
        stop_relayd(service)
        binaries.rollback(dest, RELAYD, old)
        if backup:
            scheduler.run_restore(argparse.Namespace(file=backup))
        restored = start_relayd(service, flags)
    except (services.ServiceError, SystemExit) as exc:
        print(exc, file=sys.stderr)
    lines = [f"relay update: relayd {tag} did not start. Last lines of its log:", log]
    if restored:
        lines.append(f"relayd is running {was} again, on the database from before.")
    else:
        lines.append(f"{was} did not start either; see relay scheduler status.")
    if backup:
        lines.append(f"The database from before the update is at {backup}")
    raise SystemExit("\n".join(lines))


def update_agents(dest: Path, tag: str) -> None:
    try:
        running = contribution.statuses()
        contribution.stop_all()
    except contribution.ContributionError as exc:
        raise SystemExit(f"relay update: {exc}; relay-agent was not changed") from exc
    binaries.commit(dest, AGENT, tag)
    print(f"relay-agent updated to {tag}")

    logins = {login.get("pool_id"): login for login in profile.logins()}
    failed = False
    for state in running:
        pool = state.get("pool_name") or state.get("pool_id") or "unknown pool"
        prof = logins.get(state.get("pool_id"))
        try:
            if prof is None:
                raise contribution.ContributionError("its login is no longer saved")
            pid, _ = contribution.start(
                prof, state.get("scheduler") or prof.get("scheduler")
            )
        except contribution.ContributionError as exc:
            failed = True
            print(
                f"  no longer contributing to {pool}: {exc}. "
                f"Start it with relay contribute --pool {pool}",
                file=sys.stderr,
            )
        else:
            print(f"  contributing to {pool} again (PID {pid})")
    if failed:
        raise SystemExit(1)


def run(args) -> None:
    names = [n for n in binaries.NAMES if binaries.installed_path(n).exists()]
    if not names:
        raise SystemExit(
            "relay update: relay install has not put any binaries here; "
            "run relay install first"
        )
    client = httpx.Client(timeout=120, follow_redirects=True)
    try:
        if args.version:
            tag = args.version if args.version.startswith("v") else "v" + args.version
        else:
            tag = binaries.latest_tag(client)
    except binaries.MissingBinary as exc:
        raise SystemExit(f"relay update: {exc}") from exc

    have = binaries.installed_versions()
    stale = [n for n in names if have.get(n) != tag]
    if not stale:
        print(f"{' and '.join(names)} already at {tag}")
        cli_note(tag)
        return
    for name in stale:
        print(f"{name}: {have.get(name) or 'unknown version'} -> {tag}")
    if args.check:
        cli_note(tag)
        return

    # Everything is downloaded before anything is stopped, so a failed
    # download changes nothing and the restart is as short as it can be.
    staged: dict[str, Path] = {}
    try:
        for name in stale:
            staged[name], _, _ = binaries.stage(name, tag, client)
        if RELAYD in staged:
            update_relayd(staged[RELAYD], tag, have.get(RELAYD))
        if AGENT in staged:
            update_agents(staged[AGENT], tag)
    except binaries.MissingBinary as exc:
        raise SystemExit(f"relay update: {exc}") from exc
    except httpx.HTTPStatusError as exc:
        code = exc.response.status_code
        what = f"no release {tag} for this machine" if code == 404 else f"{code}"
        raise SystemExit(f"relay update: download failed: {what}") from exc
    finally:
        for name in stale:
            binaries.discard(binaries.installed_path(name))
    cli_note(tag)
