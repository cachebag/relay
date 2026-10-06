import argparse
import os
import sys
from importlib.metadata import version

import httpx

from relay import profile
from relay.commands import (
    auth,
    credits,
    dash,
    home,
    jobs,
    logs,
    node,
    nodes,
    pool,
    status,
    submit,
)

DEFAULT_SCHEDULER = "http://localhost:8080"


def resolve(args: argparse.Namespace, env: dict, prof: dict) -> None:
    """Fill in the scheduler: flag, then $RELAY_SCHEDULER, then the saved login."""

    explicit = args.scheduler or env.get("RELAY_SCHEDULER")
    args.scheduler_given = bool(explicit)
    args.scheduler = explicit or prof.get("scheduler") or DEFAULT_SCHEDULER


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
        help=(
            "scheduler base URL (default: flag, $RELAY_SCHEDULER,"
            f"saved login, then {DEFAULT_SCHEDULER})"
        ),
    )

    sub = parser.add_subparsers(dest="subcommand")
    nodes.register(sub)
    jobs.register(sub)
    logs.register(sub)
    submit.register(sub)
    status.register(sub)
    pool.register(sub)
    dash.register(sub)
    auth.register(sub)
    credits.register(sub)
    node.register(sub)
    home.register(sub)

    args = parser.parse_args()

    try:
        resolve(args, os.environ, profile.load())
        if args.subcommand is None:
            home.run(args)
            return
        args.func(args)
    except httpx.ConnectError:
        print(
            f"relay: can't reach scheduler at {args.scheduler}",
            file=sys.stderr,
        )
        sys.exit(1)


if __name__ == "__main__":
    main()
