from datetime import UTC, datetime

import httpx

from relay import profile
from relay.commands.status import describe, requires, runtime


def register(sub) -> None:
    p = sub.add_parser("jobs", help="list jobs in your pool")
    p.set_defaults(func=run)


def fetch(scheduler: str, token: str, client: httpx.Client | None = None) -> list[dict]:
    """List the jobs in the pool the join token belongs to."""
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/jobs", headers=profile.bearer(token))
    r.raise_for_status()
    return r.json()


def seconds_ago(iso: str) -> int:
    created = datetime.fromisoformat(iso)
    return int((datetime.now(UTC) - created).total_seconds())


def age(iso: str) -> str:
    seconds = seconds_ago(iso)

    if seconds < 60:
        return f"{seconds}s"

    minutes = seconds // 60
    if minutes < 60:
        return f"{minutes}m"

    hours = minutes // 60
    if hours < 24:
        return f"{hours}h"

    days = hours // 24
    return f"{days}d"


def shorten(command: str) -> str:
    if len(command) <= 40:
        return command

    return command[:37] + "..."


def run(args) -> None:
    prof = profile.require_login()
    rows = fetch(args.scheduler, prof["pool_token"])

    print(
        f"{'ID':<16} {'STATUS':<10} {'NODE':<16} {'REQUIRES':<8} "
        f"{'ATTEMPTS':>8} {'AGE':>6} {'TIME':>7} "
        f"{'SUBMITTER':<16}  {'COMMAND'}"
    )

    for job in rows:
        command = describe(job)

        # Older jobs may not have submitter information.
        submitter = job.get("submitter") or "-"

        print(
            f"{job['id']:<16} "
            f"{job['status']:<10} "
            f"{job.get('node_id') or '-':<16} "
            f"{requires(job) or '-':<8} "
            f"{job.get('attempts', 0):>8} "
            f"{age(job['created_at']):>6} "
            f"{runtime(job) or '-':>7} "
            f"{submitter:<16}  "
            f"{shorten(command)}"
        )
