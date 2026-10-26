"""relay scheduler start | stop | status: run relayd without a terminal."""

import getpass
import os
import re

from relay import binaries, procs, profile

NAME = "relayd"
TAILSCALE_STEPS = (
    "relayd --public needs a Tailscale auth key. One-time setup at "
    "https://login.tailscale.com/admin:\n"
    "  1. DNS: MagicDNS and HTTPS Certificates on.\n"
    "  2. Settings > Keys > Generate auth key, with Reusable on.\n"
    "The key is kept in your relay config so you only paste it once."
)


def register(sub) -> None:
    p = sub.add_parser("scheduler", help="run relayd in the background")
    ps = p.add_subparsers(dest="scheduler_command", required=True)

    st = ps.add_parser("start", help="start relayd, keep its admin token and address")
    st.add_argument(
        "--public",
        action="store_true",
        help="also serve a public https address through Tailscale",
    )
    st.add_argument("--port", type=int, default=8080, help="local port (default 8080)")
    st.add_argument(
        "--name",
        default="relay",
        help="Tailscale hostname for --public (default relay)",
    )
    st.set_defaults(func=run_start)

    sp = ps.add_parser("stop", help="stop relayd")
    sp.set_defaults(func=run_stop)

    ss = ps.add_parser("status", help="is relayd running, and where")
    ss.set_defaults(func=run_status)


def authkey() -> str:
    """TS_AUTHKEY from the environment, the saved settings, or a prompt."""
    key = os.environ.get("TS_AUTHKEY") or profile.server().get("ts_authkey", "")
    if key:
        return key
    print(TAILSCALE_STEPS)
    key = getpass.getpass("Tailscale auth key (tskey-auth-...): ").strip()
    if not key:
        raise SystemExit("no auth key given; nothing started")
    profile.save_server(ts_authkey=key)
    return key


def run_start(args) -> None:
    pid = procs.running(NAME)
    if pid:
        print(f"relayd is already running (pid {pid})")
        run_status(args)
        return
    try:
        binary = binaries.find(NAME)
    except binaries.MissingBinary as exc:
        raise SystemExit(f"relay scheduler: {exc}")

    argv = [str(binary), "--port", str(args.port)]
    env = dict(os.environ)
    if args.public:
        argv += ["--public", "--name", args.name]
        env["TS_AUTHKEY"] = authkey()

    start_at = (
        procs.log_path(NAME).stat().st_size if procs.log_path(NAME).exists() else 0
    )
    procs.start(NAME, argv, env)
    address = f"http://localhost:{args.port}"
    profile.save_server(address=address)

    want = ["relayd listening on"]
    if args.public:
        want.append("relayd public address:")
    found = procs.wait_for(
        NAME, want + ["server admin token:"], 60 if args.public else 15, start_at
    )

    if procs.running(NAME) is None:
        raise SystemExit(
            "relayd stopped right away. Last lines of its log:\n" + procs.tail(NAME)
        )

    token_line = found.get("server admin token:")
    if token_line:
        token = token_line.split("server admin token:", 1)[1].strip()
        profile.save_server(admin_token=token)
    public_line = found.get("relayd public address:")
    public_url = (
        public_line.split("relayd public address:", 1)[1].strip() if public_line else ""
    )
    if public_url:
        profile.save_server(public_url=public_url)

    print(f"relayd running (pid {procs.running(NAME)})")
    print(f"  local address:   {address}")
    if args.public:
        if public_url:
            print(f"  public address:  {public_url}")
        else:
            print(
                "  public address:  not up yet; check relay scheduler status in a minute"
            )
    if token_line:
        print("  server admin token saved; relay pool create and the dashboard use it")
    elif profile.server().get("admin_token"):
        print("  server admin token: already saved")
    else:
        print(
            "  server admin token: not seen in the log; pass --admin-token to relay pool create"
        )
    print(f"  log: {procs.log_path(NAME)}")
    if not want_all_found(found, want):
        print("  (relayd is still starting; relay scheduler status shows the rest)")


def want_all_found(found: dict, want: list[str]) -> bool:
    return all(w in found for w in want)


def run_stop(args) -> None:
    if procs.stop(NAME):
        print("relayd stopped")
    else:
        print("relayd is not running")


def run_status(args) -> None:
    pid = procs.running(NAME)
    srv = profile.server()
    if pid is None:
        print("relayd is not running")
    else:
        print(f"relayd running (pid {pid})")
    if srv.get("address"):
        print(f"  local address:   {srv['address']}")
    if pid and srv.get("public_url"):
        print(f"  public address:  {srv['public_url']}")
    elif pid:
        # Maybe --public came up after start returned; look once more.
        m = re.search(r"relayd public address: (\S+)", procs.tail(NAME, 50))
        if m:
            profile.save_server(public_url=m.group(1))
            print(f"  public address:  {m.group(1)}")
    print(f"  admin token:     {'saved' if srv.get('admin_token') else 'not saved'}")
    print(f"  log: {procs.log_path(NAME)}")
