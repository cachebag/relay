import argparse
from importlib.metadata import version

from relay.commands import dash, nodes, pool


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
        "--scheduler", default="http://localhost:8080", help="scheduler base URL"
    )
    sub = parser.add_subparsers(dest="command", required=True)
    nodes.register(sub)
    pool.register(sub)
    dash.register(sub)
    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()