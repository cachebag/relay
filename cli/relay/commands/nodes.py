from datetime import datetime, timezone

import httpx

def register(sub) -> None:
    p = sub.add_parser("nodes", help="list nodes in the pool")
    p.set_defaults(func=run)

def fetch(scheduler: str, client: httpx.Client | None = None) -> list[dict]:
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/nodes")
    r.raise_for_status()
    return r.json()

def seconds_ago(iso: str) -> int:
    seen = datetime.fromisoformat(iso)
    return int((datetime.now(timezone.utc) - seen).total_seconds())

def run(args) -> None:
    rows = fetch(args.scheduler)
    print(f"{'HOST':<16} {'OS/ARCH':<14} {'CPU':>4} {'MEM':>8}  {'SEEN':>6}")
    for n in rows:
        gb = n["mem_bytes"] / 1e9
        print(f"{n['hostname']:<16} {n['os'] + '/' + n['arch']:<14} {n['cpus']:>4} {gb:>6.1f}GB  {seconds_ago(n['last_seen']):>4}s ago")