from datetime import UTC, datetime

import httpx


def register(sub) -> None:
    p = sub.add_parser("jobs", help="list jobs")
    p.add_argument("--pool", help="only show jobs in this pool id")
    p.set_defaults(func=run)


def fetch(
    scheduler: str, pool: str | None = None, client: httpx.Client | None = None
) -> list[dict]:
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/jobs", params={"pool": pool} if pool else None)
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
    rows = fetch(args.scheduler, args.pool)

    print(
        f"{'ID':<16} {'STATUS':<10} {'NODE':<16} "
        f"{'ATTEMPTS':>8} {'AGE':>6}  {'COMMAND'}"
    )

    for job in rows:
        command = " ".join(job["command"])

        print(
            f"{job['id']:<16} "
            f"{job['status']:<10} "
            f"{job.get('node_id') or '-':<16} "
            f"{job.get('attempts', 0):>8} "
            f"{age(job['created_at']):>6}  "
            f"{shorten(command)}"
        )
