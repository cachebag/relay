"""Start contributing this machine's compute power to the saved pool."""

import sys

from relay import contribution, profile

# Register the contribute command with the main Relay CLI.
def register(sub) -> None:
    parser = sub.add_parser(
        "contribute",
        help="run the agent for your current pool",
    )
    parser.set_defaults(func=run_start)


def run_start(args) -> None:
    # Use the saved login so users don't need to enter their
    # scheduler, pool token, or username again.
    prof = profile.require_login()

    # Start the background agent and report any errors without
    # displaying a Python traceback to the user.
    try:
        pid, log_path = contribution.start(prof, args.scheduler)
    except contribution.ContributionError as exc:
        print(f"relay contribute: {exc}", file=sys.stderr)
        raise SystemExit(1) from None

    # Display the pool being contributed to and the location of the agent's log file.
    pool = prof.get("pool_name", prof.get("pool_id", "?"))

    print(f"contributing to {pool} (PID {pid})")
    print(f"agent log: {log_path}")