import os

import httpx


def register(sub) -> None:
    p = sub.add_parser("pool", help="manage pools")
    ps = p.add_subparsers(dest="pool_command", required=True)

    c = ps.add_parser("create", help="create a pool and print its join token")
    c.add_argument("name")
    c.set_defaults(func=run_create)

    r = ps.add_parser("rotate-token", help="replace a pool's join token")
    r.add_argument("--pool", required=True, help="pool id")
    r.add_argument("--admin-token", help="admin token (defaults to $RELAY_ADMIN_TOKEN)")
    r.add_argument("--yes", action="store_true", help="skip the name confirmation")
    r.set_defaults(func=run_rotate_token)


def create(scheduler: str, name: str, client: httpx.Client | None = None) -> dict:
    client = client or httpx.Client(timeout=5)
    r = client.post(f"{scheduler}/pools", json={"name": name})
    r.raise_for_status()
    return r.json()


def run_create(args) -> None:
    p = create(args.scheduler, args.name)
    print(f"pool id:    {p['id']}")
    print(f"join token: {p['token']}")
    print(f"admin token: {p['admin_token']}")
    print("warning: the admin token is shown only once. Save it now.")
    print()
    print("share the token with contributors; they run:")
    print(f"relay-agent --scheduler {args.scheduler} --pool-token {p['token']}")


def get_pool_name(scheduler: str, pool_id: str, client: httpx.Client) -> str:
    r = client.get(f"{scheduler}/pools")
    r.raise_for_status()
    for p in r.json():
        if p["id"] == pool_id:
            return p["name"]
    raise SystemExit(f"no pool with id {pool_id}")


def rotate_token(
        scheduler: str,
        pool_id: str,
        admin_token: str,
        client: httpx.Client | None = None,
) -> str:
    client = client or httpx.Client(timeout=5)
    r = client.post(
        f"{scheduler}/pools/{pool_id}/token",
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    r.raise_for_status()
    return r.json()["token"]


def run_rotate_token(args) -> None:
    admin_token = args.admin_token or os.environ.get("RELAY_ADMIN_TOKEN")
    if not admin_token:
        raise SystemExit("an admin token is required: pass --admin-token or set RELAY_ADMIN_TOKEN")

    if not args.yes:
        name = get_pool_name(args.scheduler, args.pool, httpx.Client(timeout=5))
        typed = input(f"type the pool name ({name}) to confirm rotation: ").strip()
        if typed != name:
            raise SystemExit("name did not match; aborted. The join token was not changed.")

    try:
        new_token = rotate_token(args.scheduler, args.pool, admin_token)
    except httpx.HTTPStatusError as e:
        raise SystemExit(f"rotation failed: {e.response.status_code} {e.response.text.strip()}")

    print(f"new join token: {new_token}")
    print("the old join token no longer works.")
    print("restart every contributor's agent with the new token:")
    print(f"relay-agent --scheduler {args.scheduler} --pool-token {new_token}")