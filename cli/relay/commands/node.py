import os

import httpx

from relay import profile
from relay.commands.pool import get_pool_name


def register(sub) -> None:
    p = sub.add_parser("node", help="manage machines in your pool")
    ns = p.add_subparsers(dest="node_command", required=True)

    r = ns.add_parser("remove", help="remove a machine from your pool (pool admin only)")
    r.add_argument("node_id", metavar="node-id", help="id of the machine to remove")
    r.add_argument("--admin-token", help="admin token (defaults to $RELAY_ADMIN_TOKEN)")
    r.add_argument("--pool", help="pool id (defaults to the pool you logged in to)")
    r.add_argument("--yes", action="store_true", help="skip the name confirmation")
    r.set_defaults(func=run_remove)


def remove(
        scheduler: str,
        pool_id: str,
        node_id: str,
        admin_token: str,
        client: httpx.Client | None = None,
) -> None:
    client = client or httpx.Client(timeout=5)
    r = client.delete(
        f"{scheduler}/pools/{pool_id}/nodes/{node_id}",
        headers={"Authorization": f"Bearer {admin_token}"},
    )
    r.raise_for_status()


def run_remove(args) -> None:
    admin_token = args.admin_token or os.environ.get("RELAY_ADMIN_TOKEN")
    if not admin_token:
        raise SystemExit("an admin token is required: pass --admin-token or set RELAY_ADMIN_TOKEN")

    # The pool comes from the login profile, like rotate-token's --pool.
    pool_id = args.pool or profile.load().get("pool_id")
    if not pool_id:
        raise SystemExit("no pool selected: run relay login first or pass --pool")

    client = httpx.Client(timeout=5)

    # UI-11: removing someone's machine is destructive, so make the admin
    # type the pool name unless --yes was given.
    if not args.yes:
        name = get_pool_name(args.scheduler, pool_id, client)
        typed = input(f"type the pool name ({name}) to confirm removing {args.node_id}: ").strip()
        if typed != name:
            raise SystemExit("name did not match; aborted. The machine was not removed.")

    try:
        remove(args.scheduler, pool_id, args.node_id, admin_token, client)
    except httpx.HTTPStatusError as e:
        code = e.response.status_code
        if code == 401:
            raise SystemExit("removal failed: the admin token was rejected")
        if code == 404:
            raise SystemExit(f"removal failed: no machine {args.node_id} in pool {pool_id}")
        raise SystemExit(f"removal failed: {code} {e.response.text.strip()}")

    print(f"removed machine {args.node_id} from pool {pool_id}")
    print("its unfinished jobs went back to the queue without using an attempt.")
    print("its agent stops on its next heartbeat.")