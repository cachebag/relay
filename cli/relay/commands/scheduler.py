"""relay scheduler start | stop | status: run relayd without a terminal."""

import getpass
import os
import re
import shlex
import time

import httpx

from relay import binaries, procs, profile, services

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
        default=None,
        help="also serve a public https address through Tailscale",
    )
    st.add_argument("--port", type=int, default=None, help="local port (default 8080)")
    st.add_argument(
        "--name",
        default=None,
        help="Tailscale hostname for --public (default relay)",
    )
    st.add_argument(
        "--service",
        action="store_true",
        help="install or start the startup service",
    )
    st.set_defaults(func=run_start)

    sp = ps.add_parser("stop", help="stop relayd")
    sp.add_argument(
        "--service",
        action="store_true",
        help="stop and remove the startup service",
    )
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


def warm_up(public_url: str) -> bool:
    """A fresh Tailscale node has no HTTPS certificate until its first
    request, which can take half a minute. Make that request here so the
    first pool create or dashboard call doesn't time out."""
    try:
        httpx.get(f"{public_url}/healthz", timeout=60)
        return True
    except httpx.HTTPError:
        return False


def after(line: str | None, prefix: str) -> str:
    return line.split(prefix, 1)[1].strip() if line else ""


def run_start(args) -> None:
    try:
        managed = services.installed()
        if managed:
            if getattr(args, "service", False):
                start_service(args, existing=True)
            else:
                print("relayd is installed as a service; use relay scheduler status")
            return
        # Keep omitted flags distinct until an existing service has been checked.
        args.public = bool(args.public)
        args.port = 8080 if args.port is None else args.port
        args.name = "relay" if args.name is None else args.name
        if getattr(args, "service", False):
            start_service(args, existing=False)
            return
    except services.ServiceError as exc:
        raise SystemExit(f"relay scheduler: {exc}") from exc

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

    # The admin token line only appears on relayd's very first start, so it
    # is read from whatever arrived rather than waited for.
    want = ["relayd listening on"]
    if args.public:
        want.append("relayd public address:")
        print("starting relayd and joining the tailnet, up to a minute...")
    try:
        found = procs.wait_for(NAME, want, 60 if args.public else 15, start_at)
    except KeyboardInterrupt:
        raise SystemExit(
            "stopped waiting; relayd keeps running in the background. "
            "relay scheduler status shows where it got to."
        )

    if procs.running(NAME) is None:
        raise SystemExit(
            "relayd stopped right away. Last lines of its log:\n" + procs.tail(NAME)
        )

    token = after(
        procs.wait_for(NAME, ["server admin token:"], 0, start_at).get(
            "server admin token:"
        ),
        "server admin token:",
    )
    if token:
        profile.save_server(admin_token=token)
    public_url = after(found.get("relayd public address:"), "relayd public address:")
    if public_url:
        profile.save_server(public_url=public_url)
        ready = warm_up(public_url)

    print(f"relayd running (pid {procs.running(NAME)})")
    print(f"  local address:   {address}")
    if args.public:
        if public_url and ready:
            print(f"  public address:  {public_url}")
        elif public_url:
            print(
                f"  public address:  {public_url} (not answering yet; give it a minute)"
            )
        else:
            print(
                "  public address:  not up yet; check relay scheduler status in a minute"
            )
    if token:
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


def start_service(args, existing: bool) -> None:
    start_at = (
        procs.log_path(NAME).stat().st_size if procs.log_path(NAME).exists() else 0
    )
    if existing:
        argv = services.configuration()
        saved = {
            "port": int(argv[argv.index("--port") + 1]),
            "public": "--public" in argv,
            "name": argv[argv.index("--name") + 1] if "--name" in argv else "relay",
        }
        if any(
            getattr(args, name, None) is not None and getattr(args, name) != value
            for name, value in saved.items()
        ):
            raise services.ServiceError(
                f"relayd service is installed with {shlex.join(argv[1:])}; "
                "run relay scheduler stop --service first to change them"
            )
        if services.state() in {"active", "activating"}:
            print("relayd service is already running with its saved configuration")
            run_status(args)
            return
        services.start()
        print("starting relayd service with its saved configuration")
    else:
        if procs.running(NAME):
            raise services.ServiceError(
                "relayd is already running outside the service; "
                "run relay scheduler stop before installing it"
            )
        try:
            binary = binaries.find(NAME)
        except binaries.MissingBinary as exc:
            raise services.ServiceError(str(exc)) from exc
        argv = [str(binary), "--port", str(args.port)]
        key = None
        if args.public:
            argv += ["--public", "--name", args.name]
            key = authkey()
            profile.save_server(ts_authkey=key)
        services.install(argv, key)
        print("relayd service installed and starting")

    port = argv[argv.index("--port") + 1]
    public = "--public" in argv
    address = f"http://localhost:{port}"
    if public:
        profile.save_server(address=address)
    else:
        profile.save_server(address=address, public_url="")
    want = ["relayd listening on"]
    if public:
        want.append("relayd public address:")
        print("joining the tailnet, up to a minute...")
    initial_deadline = time.monotonic() + 5

    def service_is_starting() -> bool:
        state = services.state()
        return state in {"active", "activating"} or (
            state == "inactive" and time.monotonic() < initial_deadline
        )

    try:
        found = procs.wait_for(
            NAME,
            want,
            60 if public else 15,
            start_at,
            is_running=service_is_starting,
        )
    except KeyboardInterrupt:
        raise SystemExit(
            "stopped waiting; the service remains installed. "
            "relay scheduler status shows where it got to."
        )

    state = services.state()
    if state in {"inactive", "failed"}:
        raise services.ServiceError(
            "service did not start; check its log and the service manager"
        )
    capture_service_output(start_at)
    public_url = after(found.get("relayd public address:"), "relayd public address:")
    print(f"  local address:   {address}")
    if public_url:
        ready = warm_up(public_url)
        suffix = "" if ready else " (not answering yet; give it a minute)"
        print(f"  public address:  {public_url}{suffix}")
    elif public:
        print("  public address:  not up yet; check relay scheduler status")
    if not want_all_found(found, want):
        print("  service is still starting or retrying; check relay scheduler status")
    print(f"  log: {procs.log_path(NAME)}")


def capture_service_output(start_at: int | None = None) -> None:
    if start_at is None:
        start_at = services.log_offset()
    public = "--public" in services.configuration()
    token = ""
    public_url = ""
    try:
        with procs.log_path(NAME).open("rb") as log:
            log.seek(0, os.SEEK_END)
            log.seek(start_at if start_at <= log.tell() else 0)
            for raw_line in log:
                line = raw_line.decode(errors="replace")
                token_match = re.search(r"server admin token: (\S+)", line)
                if token_match:
                    token = token_match.group(1)
                if public:
                    url_match = re.search(r"relayd public address: (\S+)", line)
                    if url_match:
                        public_url = url_match.group(1)
    except FileNotFoundError:
        return
    srv = profile.server()
    updates = {}
    new_token = token and not services.token_was_captured(token)
    if new_token and token != srv.get("admin_token"):
        updates["admin_token"] = token
    if public_url and public_url != srv.get("public_url"):
        updates["public_url"] = public_url
    if updates:
        profile.save_server(**updates)
    if new_token:
        services.remember_captured_token(token)


def want_all_found(found: dict, want: list[str]) -> bool:
    return all(w in found for w in want)


def run_stop(args) -> None:
    try:
        managed = services.installed()
        if getattr(args, "service", False):
            if managed:
                services.remove()
                print("relayd service stopped and removed")
            else:
                print("relayd service is not installed")
            return
        if managed:
            if services.stop():
                print("relayd service stopped; it remains registered for startup")
            else:
                print(
                    "relayd service is not running; it remains registered for startup"
                )
            return
    except services.ServiceError as exc:
        raise SystemExit(f"relay scheduler: {exc}") from exc
    if procs.stop(NAME):
        print("relayd stopped")
    else:
        print("relayd is not running")


def run_status(args) -> None:
    try:
        if services.installed():
            state = services.state()
            capture_service_output()
            srv = profile.server()
            labels = {
                "active": "running",
                "activating": "starting or retrying",
                "reloading": "reloading",
                "deactivating": "stopping",
                "inactive": "stopped",
                "failed": "failed",
            }
            print(f"relayd service {labels[state]} (installed for startup)")
            if srv.get("address"):
                print(f"  local address:   {srv['address']}")
            if srv.get("public_url"):
                print(f"  public address:  {srv['public_url']}")
            print(
                f"  admin token:     {'saved' if srv.get('admin_token') else 'not saved'}"
            )
            print(f"  log: {procs.log_path(NAME)}")
            return
    except services.ServiceError as exc:
        raise SystemExit(f"relay scheduler: {exc}") from exc
    pid = procs.running(NAME)
    srv = profile.server()
    if pid is None:
        print("relayd is not running")
    else:
        print(f"relayd running (pid {pid})")
    if srv.get("address"):
        print(f"  local address:   {srv['address']}")
    if pid:
        # The log is the truth for the public address: a restart can come up
        # under a different name than the one saved last time.
        lines = re.findall(r"relayd public address: (\S+)", procs.tail(NAME, 200))
        public_url = lines[-1] if lines else srv.get("public_url", "")
        if public_url and public_url != srv.get("public_url"):
            profile.save_server(public_url=public_url)
        if public_url:
            print(f"  public address:  {public_url}")
    print(f"  admin token:     {'saved' if srv.get('admin_token') else 'not saved'}")
    print(f"  log: {procs.log_path(NAME)}")
