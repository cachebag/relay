import shutil
import sys
from pathlib import Path

from relay import contribution, procs, profile, services
from relay.commands import scheduler

# Kept in the config folder unless --tailscale is given.
KEEP = ("bin", "tsnet")


def register(sub) -> None:
    p = sub.add_parser(
        "reset",
        help="stop relay and wipe its pools, logins and jobs on this machine",
    )
    p.add_argument(
        "-y", "--yes", action="store_true", help="don't ask for confirmation"
    )
    p.add_argument(
        "--tailscale",
        action="store_true",
        help="also forget the Tailscale identity and auth key (new public address)",
    )
    p.set_defaults(func=run)


def agent_cache_root() -> Path:
    """The agents' cache root: job folders and the machine's node ID.
    Wiping it gives this machine a new node ID, so no old pool holds it."""
    return contribution._agent_cache_dir("x").parent.parent


def stop_everything() -> list[str]:
    """Stop every agent and the scheduler. Returns what was stopped; keeps
    going when one of them can't be stopped."""
    done = []
    try:
        for agent in contribution.stop_all():
            done.append(
                f"stopped contributing to {agent.get('pool_name') or agent.get('pool_id')}"
            )
    except contribution.ContributionError as exc:
        print(f"warning: {exc}", file=sys.stderr)
    try:
        if services.installed():
            services.remove()
            done.append("removed the relayd service")
    except (services.ServiceError, OSError) as exc:
        print(f"warning: could not remove the relayd service: {exc}", file=sys.stderr)
    try:
        if procs.stop(scheduler.NAME):
            done.append("stopped relayd")
    except OSError as exc:
        print(f"warning: could not stop relayd: {exc}", file=sys.stderr)
    return done


def wipe(keep_tailscale: bool) -> None:
    """Delete the config folder's contents except bin/ (and tsnet/ plus the
    Tailscale auth key when keep_tailscale), then the agent cache root."""
    authkey = profile.server().get("ts_authkey") if keep_tailscale else None
    keep = KEEP if keep_tailscale else ("bin",)
    config = profile.config_dir()
    if config.exists():
        for entry in config.iterdir():
            if entry.name in keep:
                continue
            if entry.is_dir() and not entry.is_symlink():
                shutil.rmtree(entry, ignore_errors=True)
            else:
                entry.unlink(missing_ok=True)
    shutil.rmtree(agent_cache_root(), ignore_errors=True)
    if authkey:
        profile.save_server(ts_authkey=authkey)


def run(args) -> None:
    config = profile.config_dir()
    keep = "bin/" if args.tailscale else "bin/, tsnet/ and the Tailscale key"
    if not args.yes:
        print(
            "this stops relayd and every agent on this machine, then deletes "
            f"pools, logins, tokens, logs and job folders in {config}\n"
            f"(keeps {keep})"
        )
        try:
            answer = input("reset relay? [y/N] ")
        except EOFError:
            answer = ""
        if answer.strip().lower() not in ("y", "yes"):
            print("reset cancelled")
            return
    for line in stop_everything():
        print(line)
    wipe(keep_tailscale=not args.tailscale)
    print(f"relay reset done (kept {keep})")
    print("next: relay scheduler start --public, then relay pool create <name>")
