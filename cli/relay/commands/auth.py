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

    o = sub.add_parser("logout", help="forget the current login")
    o.set_defaults(func=run_logout)

    ls = sub.add_parser("pools", help="list the pools you are logged in to")
    ls.set_defaults(func=run_pools)

    u = sub.add_parser("use", help="switch to another pool you are logged in to")
    u.add_argument("pool", help="pool name or ID, see relay pools")
    u.set_defaults(func=run_use)


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


def describe(login: dict) -> str:
    return f"{login.get('pool_name', '?')} ({login.get('pool_id', '?')}) at {login.get('scheduler', '?')}"


def run_whoami(args) -> None:
    prof = profile.load()
    if not prof:
        print("not logged in")
        sys.exit(1)
    print(f"name:      {prof['name']}")
    print(f"pool:      {prof['pool_name']} ({prof['pool_id']})")
    print(f"scheduler: {prof['scheduler']}")
    others = [
        login for login in profile.logins() if login is not prof and login != prof
    ]
    if others:
        print(
            f"also in:   {len(others)} other pool{'s' if len(others) > 1 else ''}, see relay pools"
        )


def run_pools(args) -> None:
    logins = profile.logins()
    if not logins:
        print("not logged in, run relay login first")
        sys.exit(1)
    current = profile.load()
    print(f"  {'NAME':<16} {'ID':<14} SCHEDULER")
    for login in logins:
        mark = "*" if login == current else " "
        print(
            f"{mark} {login.get('pool_name', '?'):<16} "
            f"{login.get('pool_id', '?'):<14} {login.get('scheduler', '?')}"
        )


def run_use(args) -> None:
    try:
        login = profile.use(args.pool)
    except KeyError:
        print(f"no saved login for {args.pool}; run relay pools", file=sys.stderr)
        sys.exit(1)
    print(f"now using pool {describe(login)}")


def run_logout(args) -> None:
    remaining = profile.delete()
    print("logged out")
    if remaining:
        print(f"now using pool {describe(remaining)}")
