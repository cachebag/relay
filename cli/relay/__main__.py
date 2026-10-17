import argparse
import os
import sys
from importlib.metadata import version

import httpx

from relay import profile
from relay.commands import auth, credits, dash, jobs, logs, nodes, pool, status, submit

DEFAULT_SCHEDULER = "http://localhost:8080"

# Commands whose --pool falls back to the logged-in pool.
POOL_COMMANDS = {"nodes", "jobs", "dash"}


def resolve(args: argparse.Namespace, env: dict, prof: dict) -> None:
    """Fill in the scheduler (flag, env, profile, default) and pool (flag, profile)."""
    
    explicit = args.scheduler or env.get("RELAY_SCHEDULER")
    args.scheduler_given = bool(explicit)
    args.scheduler = explicit or prof.get("scheduler") or DEFAULT_SCHEDULER
    if args.subcommand in POOL_COMMANDS:
        args.pool = args.pool or prof.get("pool_id")


def main() -> None:
    parser = argparse.ArgumentParser(prog="relay")
    parser.add_argument(
        "-v",
        "--version",
        action="version",
        version=f"relay {version('relay')}",
        help="show relay version",
    )
    parser.add_argument(
        "--scheduler",
        default=None,
        help=("scheduler base URL (default: flag, $RELAY_SCHEDULER,"
            f"saved login, then {DEFAULT_SCHEDULER})"),
    )

    sub = parser.add_subparsers(dest="subcommand", required=True)
    nodes.register(sub)
    jobs.register(sub)
    logs.register(sub)
    submit.register(sub)
    status.register(sub)
    pool.register(sub)
    dash.register(sub)
    auth.register(sub)
    credits.register(sub)

    args = parser.parse_args()

    try:
        resolve(args, os.environ, profile.load())
        args.func(args)
    except httpx.ConnectError:
        print(
            f"relay: can't reach scheduler at {args.scheduler}",
            file=sys.stderr,
        )
        sys.exit(1)


if __name__ == "__main__":
    main()