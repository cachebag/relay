import argparse
import getpass
import sys

import httpx

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("login", help="save your scheduler, pool, and name")
    p.add_argument(
        "--scheduler",
        default=argparse.SUPPRESS,
        help="scheduler base URL (prompted if omitted)",
    )
    p.add_argument("--pool-token", help="pool join token (prompted if omitted)")
    p.add_argument("--name", help="your name (default: your OS user)")
    p.set_defaults(func=run_login)

    w = sub.add_parser("whoami", help="show the saved login")
    w.set_defaults(func=run_whoami)

    o = sub.add_parser("logout", help="forget the saved login")
    o.set_defaults(func=run_logout)


def fetch_pool(scheduler: str, token: str, client: httpx.Client | None = None) -> dict:
    """Ask the scheduler which pool owns this token."""

    client = client or httpx.Client(timeout=5)
    r = client.get(
        f"{scheduler.rstrip('/')}/pools/self",
        headers={"Authorization": f"Bearer {token}"},
    )
    r.raise_for_status()
    return r.json()


def login(
    scheduler: str, token: str, name: str, client: httpx.Client | None = None
) -> dict:
    """Check the token and save the profile. Raises httpx.HTTPStatusError on failure."""

    pool = fetch_pool(scheduler, token, client)
    prof = {
        "scheduler": scheduler,
        "pool_id": pool["id"],
        "pool_name": pool["name"],
        "pool_token": token,
        "name": name,
    }
    profile.save(prof)
    return prof


def run_login(args) -> None:

    # args.scheduler is already resolved by main(): flag, env, profile, default.
    scheduler = args.scheduler
    if not args.scheduler_given:
        scheduler = input(f"scheduler URL [{scheduler}]: ").strip() or scheduler
    token = args.pool_token or getpass.getpass("pool token: ")
    name = args.name or getpass.getuser()

    try:
        prof = login(scheduler, token, name)
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 403:
            print("invalid pool token", file=sys.stderr)
        else:
            print(f"login failed: {exc}", file=sys.stderr)
        sys.exit(1)
    except (httpx.HTTPError, httpx.InvalidURL) as exc:
        print(f"login failed: {exc}", file=sys.stderr)
        sys.exit(1)

    print(
        f"logged in to pool {prof['pool_name']} ({prof['pool_id']}) "
        f"at {prof['scheduler']} as {prof['name']}"
    )


def run_whoami(args) -> None:
    prof = profile.load()
    if not prof:
        print("not logged in")
        sys.exit(1)
    print(f"name:      {prof['name']}")
    print(f"pool:      {prof['pool_name']} ({prof['pool_id']})")
    print(f"scheduler: {prof['scheduler']}")


def run_logout(args) -> None:
    profile.delete()
    print("logged out")
