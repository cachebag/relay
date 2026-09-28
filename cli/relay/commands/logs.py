import sys

import httpx


def register(sub) -> None:
    p = sub.add_parser("logs", help="show a job's output")
    p.add_argument("id", help="job id")
    p.set_defaults(func=run)


def run(args, client: httpx.Client | None = None) -> None:
    client = client or httpx.Client(timeout=5)

    r = client.get(f"{args.scheduler}/jobs/{args.id}/output")

    if r.status_code == 404:
        print("no such job", file=sys.stderr)
        sys.exit(1)

    if r.status_code == 409:
        print(f"job {args.id} has not finished", file=sys.stderr)
        sys.exit(1)

    r.raise_for_status()

    result = r.json()

    stdout = result.get("stdout", "")
    stderr = result.get("stderr", "")
    exit_code = result.get("exit_code", 0)

    if stdout:
        print(stdout, end="")

    if stderr:
        print(stderr, end="", file=sys.stderr)

    sys.exit(exit_code)