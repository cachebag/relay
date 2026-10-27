"""Start contributing this machine's compute power to the saved pool."""

import sys

from relay import contribution, profile


# Register the contribute command and its subcommands with the main Relay CLI.
def register(sub) -> None:
    parser = sub.add_parser(
        "contribute",
        help="run the agent for your current pool",
    )
    parser.set_defaults(func=run_start)

    # Register the commands used to stop and inspect the local agent.
    actions = parser.add_subparsers(dest="contribute_action")

    stop_parser = actions.add_parser(
        "stop",
        help="stop the local agent",
    )
    stop_parser.set_defaults(func=run_stop)

    status_parser = actions.add_parser(
        "status",
        help="show local agent status",
    )
    status_parser.set_defaults(func=run_status)


# Display both the pool name and ID when they are available.
def _pool_label(state: dict) -> str:
    name = state.get("pool_name")
    pool_id = state.get("pool_id")

    if name and pool_id:
        return f"{name} ({pool_id})"

    return name or pool_id or "unknown pool"


def run_start(args) -> None:
    # Use the saved login so users don't need to enter their scheduler, pool token, or username again.
    prof = profile.require_login()

    # Start the background agent and report any errors without displaying a Python traceback to the user.
    try:
        pid, log_path = contribution.start(prof, args.scheduler)
    except contribution.ContributionError as exc:
        print(f"relay contribute: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    # Display the pool being contributed to and the location of the agent's log file.
    pool = prof.get("pool_name", prof.get("pool_id", "?"))

    print(f"contributing to {pool} (PID {pid})")
    print(f"agent log: {log_path}")


def run_stop(args) -> None:
    # Stop the saved agent process and report the result to the user.
    try:
        stopped = contribution.stop()
    except contribution.ContributionError as exc:
        print(f"relay contribute stop: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    if stopped is None:
        print("not contributing")
    else:
        print(f"stopped contributing to {_pool_label(stopped)} (PID {stopped['pid']})")


def run_status(args) -> None:
    # Check whether this machine is currently contributing to a pool.
    try:
        running = contribution.status()
    except contribution.ContributionError as exc:
        print(f"relay contribute status: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    if running is None:
        print("not contributing")
    else:
        print(f"contributing to {_pool_label(running)} (PID {running['pid']})")
