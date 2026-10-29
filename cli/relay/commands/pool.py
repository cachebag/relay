import os
import sys
from ipaddress import ip_address
from urllib.parse import urlsplit, urlunsplit

import httpx

from relay import profile

ADMIN_TOKEN_HINT = (
    "a server admin token is required: pass --admin-token, set RELAY_ADMIN_TOKEN, "
    "or start the scheduler with relay scheduler start, which saves it"
)


def invite_scheduler_url(scheduler: str) -> tuple[str, bool]:
    """Return a shareable scheduler URL and whether its host was replaced."""
    parts = urlsplit(scheduler)
    host = parts.hostname
    if host is None:
        return scheduler, False

    loopback = host.rstrip(".").lower() == "localhost"
    if not loopback:
        try:
            loopback = ip_address(host).is_loopback
        except ValueError:
            pass

    if not loopback:
        return scheduler, False

    port = f":{parts.port}" if parts.port is not None else ""
    return urlunsplit(parts._replace(netloc=f"<your address>{port}")), True


def print_agent_invite(scheduler: str, token: str, public_url: str = "") -> None:
    """Print the contributor command, with a hint for local schedulers.

    When relayd runs with --public it sends its public address, and the
    invite uses that as-is, with no hint (#113).
    """
    if public_url:
        print(f"relay-agent --scheduler {public_url} --pool-token {token}")
        return

    invite_url, needs_address = invite_scheduler_url(scheduler)
    print(f"relay-agent --scheduler {invite_url} --pool-token {token}")
    if needs_address:
        print(
            "replace <your address> with an address contributors can reach, "
            "for example your Tailscale name."
        )


def register(sub) -> None:
    p = sub.add_parser("pool", help="manage pools")
    ps = p.add_subparsers(dest="pool_command", required=True)

    c = ps.add_parser("create", help="create a pool and print its join token")
    c.add_argument("name")
    c.add_argument(
        "--admin-token",
        help="server admin token printed by relayd (defaults to $RELAY_ADMIN_TOKEN)",
    )
    c.set_defaults(func=run_create)

    r = ps.add_parser("rotate-token", help="replace a pool's join token")
    r.add_argument("--pool", help="pool id (defaults to the pool you logged in to)")
    r.add_argument(
        "--admin-token", help="pool admin token (defaults to $RELAY_ADMIN_TOKEN)"
    )
    r.add_argument("--yes", action="store_true", help="skip the name confirmation")
    r.set_defaults(func=run_rotate_token)


def server_admin_token(flag: str | None) -> str:
    """The server admin token from the flag or the environment, or exit 1."""
    token = (
        flag
        or os.environ.get("RELAY_ADMIN_TOKEN")
        or profile.server().get("admin_token")
    )
    if not token:
        print(ADMIN_TOKEN_HINT, file=sys.stderr)
        sys.exit(1)
    return token


def create(
    scheduler: str, name: str, admin_token: str, client: httpx.Client | None = None
) -> dict:
    """Create a pool. Only the server admin token is allowed to."""
    client = client or httpx.Client(timeout=5)
    r = client.post(
        f"{scheduler}/pools", json={"name": name}, headers=profile.bearer(admin_token)
    )
    r.raise_for_status()
    return r.json()


def run_create(args) -> None:
    admin_token = server_admin_token(args.admin_token)
    try:
        p = create(args.scheduler, args.name, admin_token)
    except httpx.HTTPStatusError as e:
        if e.response.status_code == 401:
            raise SystemExit("create failed: the server admin token was rejected")
        raise SystemExit(
            f"create failed: {e.response.status_code} {e.response.text.strip()}"
        )
    print(f"pool id:    {p['id']}")
    print(f"join token: {p['token']}")
    print(f"admin token: {p['admin_token']}")
    print("warning: the admin token is shown only once. Save it now.")
    print()
    print("share the token with contributors; they run:")
    print_agent_invite(args.scheduler, p["token"], p.get("public_url") or "")


def pool_from_login(flag: str | None) -> tuple[str, str]:
    """The pool to act on: --pool, or the one in the saved login.

    Returns the pool ID and the name to confirm with. The name only comes
    from the login, since listing pools needs the server admin token.
    """
    prof = profile.load()
    pool_id = flag or prof.get("pool_id")
    if not pool_id:
        raise SystemExit("no pool selected: run relay login first or pass --pool")
    name = prof.get("pool_name", "") if pool_id == prof.get("pool_id") else ""
    return pool_id, name


def confirm_pool(pool_id: str, name: str, what: str) -> None:
    """Make the admin type the pool name before a destructive action (UI-11)."""
    if not name:
        raise SystemExit(
            f"can't confirm pool {pool_id}: log in to it first, or pass --yes"
        )
    typed = input(f"type the pool name ({name}) to confirm {what}: ").strip()
    if typed != name:
        raise SystemExit("name did not match; aborted. Nothing was changed.")


def rotate_token(
    scheduler: str,
    pool_id: str,
    admin_token: str,
    client: httpx.Client | None = None,
) -> str:
    client = client or httpx.Client(timeout=5)
    r = client.post(
        f"{scheduler}/pools/{pool_id}/token",
        headers=profile.bearer(admin_token),
    )
    r.raise_for_status()
    return r.json()["token"]


def rotate(
    scheduler: str,
    pool_id: str,
    admin_token: str,
    client: httpx.Client | None = None,
) -> dict:
    """Rotate the join token and return the whole response.

    Same request as rotate_token, but the caller also gets public_url,
    relayd's public address ("" when it isn't running with --public).
    """
    client = client or httpx.Client(timeout=5)
    r = client.post(
        f"{scheduler}/pools/{pool_id}/token",
        headers=profile.bearer(admin_token),
    )
    r.raise_for_status()
    return r.json()


def run_rotate_token(args) -> None:
    admin_token = args.admin_token or os.environ.get("RELAY_ADMIN_TOKEN")
    if not admin_token:
        raise SystemExit(
            "an admin token is required: pass --admin-token or set RELAY_ADMIN_TOKEN"
        )

    pool_id, name = pool_from_login(args.pool)
    if not args.yes:
        confirm_pool(pool_id, name, "rotation")

    try:
        rotated = rotate(args.scheduler, pool_id, admin_token)
        new_token = rotated["token"]
    except httpx.HTTPStatusError as e:
        raise SystemExit(
            f"rotation failed: {e.response.status_code} {e.response.text.strip()}"
        )

    print(f"new join token: {new_token}")
    print("the old join token no longer works.")
    print("restart every contributor's agent with the new token:")
    print_agent_invite(args.scheduler, new_token, rotated.get("public_url") or "")

