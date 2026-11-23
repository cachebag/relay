import sys
from datetime import UTC, datetime

import httpx

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("nodes", help="list nodes in your pool")
    p.set_defaults(func=run)


def fetch(scheduler: str, token: str, client: httpx.Client | None = None) -> list[dict]:
    """List the nodes in the pool the join token belongs to."""
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/nodes", headers=profile.bearer(token))
    r.raise_for_status()
    return r.json()


def seconds_ago(iso: str) -> int:
    seen = datetime.fromisoformat(iso)
    return int((datetime.now(UTC) - seen).total_seconds())


def status(iso: str) -> str:
    return "online" if seconds_ago(iso) < 30 else "offline"


def run(args) -> None:
    prof = profile.require_login()
    try:
        rows = fetch(args.scheduler, prof["pool_token"])
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code != 403:
            raise
        if "pool token was rotated" in exc.response.text.lower():
            print(
                "relay: pool token was rotated; ask the admin for the new invite. "
                "Run 'relay login' with the new token.",
                file=sys.stderr,
            )
        else:
            print(
                "relay: invalid pool token. Run 'relay login' again.",
                file=sys.stderr,
            )
        sys.exit(1)
    print(
        f"{'HOST':<16} {'ID':<12} {'STATUS':<8} {'OS/ARCH':<14} {'CPU':>4} {'MEM':>8}  {'SEEN':>9}  {'POOL':<16}"
    )
    for n in rows:
        gb = n["mem_bytes"] / 1e9
        print(
            f"{n['hostname']:<16} "
            f"{n['id']:<12} "
            f"{n.get('status') or status(n['last_seen']):<8} "
            f"{n['os'] + '/' + n['arch']:<14} "
            f"{n['cpus']:>4} "
            f"{gb:>6.1f}GB  "
            f"{seconds_ago(n['last_seen']):>4}s ago  "
            f"{n.get('pool_id', ''):<16}"
        )
