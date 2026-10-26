import sys

import httpx

from relay import binaries


def register(sub) -> None:
    p = sub.add_parser(
        "install", help="download relayd and relay-agent for this machine"
    )
    p.add_argument(
        "--version", help="release tag, for example v0.2.5 (default: latest)"
    )
    p.add_argument(
        "--agent-only", action="store_true", help="only relay-agent, for contributors"
    )
    p.set_defaults(func=run)


def run(args) -> None:
    names = ("relay-agent",) if args.agent_only else binaries.NAMES
    for name in names:
        try:
            path, tag = binaries.download(name, args.version)
        except binaries.MissingBinary as exc:
            print(f"relay install: {exc}", file=sys.stderr)
            sys.exit(1)
        except httpx.HTTPStatusError as exc:
            code = exc.response.status_code
            if code == 404 and args.version:
                what = f"no release {args.version}"
            elif code == 404:
                what = (
                    "the latest release has no build for this machine, "
                    "or the repo is not public"
                )
            else:
                what = f"download failed: {code}"
            print(f"relay install: {what} ({name})", file=sys.stderr)
            sys.exit(1)
        print(f"{name} {tag} -> {path}")
    print("run them from there, or add that folder to your PATH")
