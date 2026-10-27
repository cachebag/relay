import argparse
import os
import sys
from importlib.metadata import version

import httpx

from relay import profile
from relay.commands import (
    auth,
    contribute,
    credits,
    dash,
    install,
    jobs,
    logs,
    node,
    nodes,
    pool,
    scheduler,
    status,
    submit,
)

DEFAULT_SCHEDULER = "http://localhost:8080"


def resolve(
    args: argparse.Namespace, env: dict, prof: dict, server: dict | None = None
) -> None:
    """Fill in the scheduler: flag, then $RELAY_SCHEDULER, then the saved
    login, then the scheduler this machine runs (its public address first)."""

    server = server or {}
    explicit = args.scheduler or env.get("RELAY_SCHEDULER")
    args.scheduler_given = bool(explicit)
    args.scheduler = (
        explicit
        or prof.get("scheduler")
        or server.get("public_url")
        or server.get("address")
        or DEFAULT_SCHEDULER
    )


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
    contribute.register(sub)
    credits.register(sub)
    node.register(sub)
    install.register(sub)
    scheduler.register(sub)

    args = parser.parse_args()

    try:
        resolve(args, os.environ, profile.load(), profile.server())
        if args.subcommand is None:
            dash.run(args)
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
