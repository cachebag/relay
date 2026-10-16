from datetime import UTC, datetime

import httpx


def register(sub) -> None:
    p = sub.add_parser("nodes", help="list nodes in the pool")
    p.add_argument("--pool", help="only show nodes in this pool id")
    p.set_defaults(func=run)


def fetch(
    scheduler: str, pool: str | None = None, client: httpx.Client | None = None
) -> list[dict]:
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/nodes", params={"pool": pool} if pool else None)
    r.raise_for_status()
    return r.json()


def seconds_ago(iso: str) -> int:
    seen = datetime.fromisoformat(iso)
    return int((datetime.now(UTC) - seen).total_seconds())


def status(iso: str) -> str:
    return "online" if seconds_ago(iso) < 30 else "offline"


def run(args) -> None:
    rows = fetch(args.scheduler, args.pool)
    print(
        f"{'HOST':<16} {'STATUS':<8} {'OS/ARCH':<14} {'CPU':>4} {'MEM':>8}  {'SEEN':>9}  {'POOL':<16}"
    )
    for n in rows:
        gb = n["mem_bytes"] / 1e9
        print(
            f"{n['hostname']:<16} "
            f"{n.get('status') or status(n['last_seen']):<8} "
            f"{n['os'] + '/' + n['arch']:<14} "
            f"{n['cpus']:>4} "
            f"{gb:>6.1f}GB  "
            f"{seconds_ago(n['last_seen']):>4}s ago  "
            f"{n.get('pool_id', ''):<16}"
        )
