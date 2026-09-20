from datetime import UTC, datetime

import httpx


def register(sub) -> None:
    p = sub.add_parser("nodes", help="list nodes in the pool")
    p.add_argument("--pool", help="Only show nodes in this pool id")
    p.set_defaults(func=run)


def fetch(scheduler: str, client: httpx.Client | None = None) -> list[dict]:
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/nodes")
    r.raise_for_status()
    return r.json()


def seconds_ago(iso: str) -> int:
    seen = datetime.fromisoformat(iso)
    return int((datetime.now(UTC) - seen).total_seconds())

def status(iso: str) -> str:
    return "online" if seconds_ago(iso) < 30 else "offline"


def run(args) -> None:
    rows = fetch(args.scheduler)
    print(f"{'HOST':<16} {'OS/ARCH':<14} {'CPU':>4} {'MEM':>8}  {'SEEN':>6}")
    for n in rows:
        gb = n["mem_bytes"] / 1e9
        print(
            f"{n['hostname']:<16} {n['os'] + '/' + n['arch']:<14} {n['cpus']:>4} {gb:>6.1f}GB  {seconds_ago(n['last_seen']):>4}s ago"
        )
