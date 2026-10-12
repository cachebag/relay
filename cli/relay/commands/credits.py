import sys

import httpx

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("credits", help="show credit balances in your pool")
    p.set_defaults(func=run)


def fetch(
    scheduler: str, pool: str, token: str, client: httpx.Client | None = None
) -> list[dict]:
    client = client or httpx.Client(timeout=5)
    r = client.get(
        f"{scheduler}/pools/{pool}/credits",
        headers={"Authorization": f"Bearer {token}"},
    )
    r.raise_for_status()
    return r.json()


def run(args) -> None:
    prof = profile.load()
    if not prof:
        print("not logged in, run relay login first", file=sys.stderr)
        sys.exit(1)

    rows = fetch(args.scheduler, prof["pool_id"], prof["pool_token"])

    # The logged-in user's row is marked with a *.
    print(f"  {'ACCOUNT':<16} {'BALANCE':>8}")
    for row in rows:
        mark = "*" if row["account"] == prof.get("name") else " "
        print(f"{mark} {row['account']:<16} {row['balance']:>8}")
