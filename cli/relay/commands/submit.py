
import sys

import httpx

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("submit", help="submit a job to the pool")
    p.add_argument("command", nargs="+", help="command to run, after --")
    p.set_defaults(func=run)


def submit(
    scheduler: str,
    command: list[str],
    token: str,
    submitter: str,
    client: httpx.Client | None = None,
) -> dict:
    """Submit a job using the logged-in pool token and user name."""
    client = client or httpx.Client(timeout=5)

    r = client.post(
        f"{scheduler.rstrip('/')}/jobs",
        headers={"Authorization": f"Bearer {token}"},
        json={
            "command": command,
            "submitter": submitter,
        },
    )

    r.raise_for_status()
    return r.json()


def run(args) -> None:
    """Submit a job using the saved login profile."""
    prof = profile.load()

    token = prof.get("pool_token")
    name = prof.get("name")

    # A saved pool token and user name are required.
    if (
        not isinstance(token, str)
        or not token.strip()
        or not isinstance(name, str)
        or not name.strip()
    ):
        print("relay: not logged in. Run 'relay login' first.", file=sys.stderr)
        sys.exit(1)

    try:
        job = submit(
            args.scheduler,
            args.command,
            token,
            name,
        )
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 403:
            print(
                "relay: invalid pool token. Run 'relay login' again.",
                file=sys.stderr,
            )
        else:
            print(f"relay: job submission failed: {exc}", file=sys.stderr)
        sys.exit(1)

    print(f"{job['id']}  {job['status']}")
