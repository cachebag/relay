"""Start, list, and stop this machine's agents for saved pools."""

import sys

from relay import contribution, profile


# Register the contribute command and its subcommands with the main Relay CLI.
def register(sub) -> None:
    parser = sub.add_parser(
        "contribute",
        help="run one local agent per pool",
    )
    parser.add_argument(
        "--pool",
        metavar="NAME",
        help="saved pool name or ID (default: current login)",
    )
    parser.set_defaults(func=run_start)

    # Register the commands used to stop and inspect local agents.
    actions = parser.add_subparsers(dest="contribute_action")

    stop_parser = actions.add_parser(
        "stop",
        help="stop one pool's agent or all local agents",
    )
    target = stop_parser.add_mutually_exclusive_group()
    target.add_argument(
        "--pool",
        metavar="NAME",
        help="pool name or ID (default: current login)",
    )
    target.add_argument("--all", action="store_true", help="stop agents for every pool")
    stop_parser.set_defaults(func=run_stop)

    status_parser = actions.add_parser(
        "status",
        help="list all running local agents",
    )
    status_parser.set_defaults(func=run_status)


# Display both the pool name and ID when they are available.
def _pool_label(state: dict) -> str:
    name = state.get("pool_name")
    pool_id = state.get("pool_id")

    if name and pool_id:
        return f"{name} ({pool_id})"

    return name or pool_id or "unknown pool"


def _saved_login(pool: str) -> dict:
    """Select a saved login without changing the current pool."""
    for field in ("pool_id", "pool_name"):
        matches = [login for login in profile.logins() if login.get(field) == pool]
        if matches:
            if len(matches) > 1:
                raise contribution.ContributionError(
                    f"pool {pool!r} is ambiguous; use its pool ID"
                )
            return matches[0]
    raise contribution.ContributionError(
        f"pool {pool!r} is not saved; run relay login for that pool first"
    )


def _stop_pool_id(pool: str) -> str:
    """Allow stopping a running pool even if its saved login was removed."""
    for field in ("pool_id", "pool_name"):
        matches = [
            running for running in contribution.statuses() if running.get(field) == pool
        ]
        if matches:
            if len(matches) > 1:
                raise contribution.ContributionError(
                    f"pool {pool!r} is ambiguous; use its pool ID"
                )
            return matches[0]["pool_id"]
    return _saved_login(pool)["pool_id"]


def run_start(args) -> None:
    # A named pool has its own scheduler and credentials; 
    # selecting it does not change the current login used by other commands.
    try:
        requested = getattr(args, "pool", None)
        prof = _saved_login(requested) if requested else profile.require_login()
        scheduler = (
            args.scheduler
            if getattr(args, "scheduler_given", False)
            else prof.get("scheduler") or args.scheduler
        )
        pid, log_path = contribution.start(prof, scheduler)
    except contribution.AgentExitedError as exc:
        print(
            "relay contribute: relay-agent exited during startup",
            file=sys.stderr,
        )
        print(exc.line, file=sys.stderr)
        raise SystemExit(1) from None
    except contribution.ContributionError as exc:
        print(f"relay contribute: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    # Display the pool being contributed to and the location of the agent's log file.
    pool = prof.get("pool_name", prof.get("pool_id", "?"))

    print(f"contributing to {pool} (PID {pid})")
    print(f"agent log: {log_path}")


def run_stop(args) -> None:
    # No pool means the current login; 
    # --all includes other logins and even agents whose logins were removed after they were started.
    try:
        if getattr(args, "all", False):
            stopped_agents = contribution.stop_all()
        else:
            requested = getattr(args, "pool", None)
            pool_id = (
                _stop_pool_id(requested) if requested else profile.load().get("pool_id")
            )
            stopped = contribution.stop(pool_id) if pool_id else None
            stopped_agents = [stopped] if stopped is not None else []
    except contribution.ContributionError as exc:
        print(f"relay contribute stop: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    if not stopped_agents:
        print("not contributing")
    else:
        for stopped in stopped_agents:
            print(
                f"stopped contributing to {_pool_label(stopped)} (PID {stopped['pid']})"
            )


def run_status(args) -> None:
    # Show every running agent, not just the agent for the current login.
    try:
        running_agents = contribution.statuses()
    except contribution.ContributionError as exc:
        print(f"relay contribute status: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    if not running_agents:
        print("not contributing")
    else:
        for running in running_agents:
            scheduler = running.get("scheduler")
            suffix = f" at {scheduler}" if scheduler else ""
            print(
                f"contributing to {_pool_label(running)} (PID {running['pid']}){suffix}"
            )
