import argparse
import os
import sys
from importlib.metadata import version

import httpx

from relay.commands import dash, jobs, logs, nodes, pool, status, submit


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
        default=os.environ.get("RELAY_SCHEDULER") or "http://localhost:8080",
        help="scheduler base URL (env: RELAY_SCHEDULER)",
    )
    sub = parser.add_subparsers(dest="command", required=True)
    nodes.register(sub)
    jobs.register(sub)
    logs.register(sub)
    submit.register(sub)
    status.register(sub)
    pool.register(sub)
    dash.register(sub)

    args = parser.parse_args()

    try:
        args.func(args)
    except httpx.ConnectError:
        print(
            f"relay: can't reach scheduler at {args.scheduler}",
            file=sys.stderr,
        )
        sys.exit(1)


if __name__ == "__main__":
    main()