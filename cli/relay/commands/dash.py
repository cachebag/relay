import os

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("dash", help="live dashboard of your pool")
    p.add_argument(
        "--admin-token",
        help="server admin token, only needed to create pools (defaults to $RELAY_ADMIN_TOKEN)",
    )
    p.set_defaults(func=run)


def run(args) -> None:
    """Open the dashboard. With no saved login it asks you to join or create a pool."""
    from relay.dash import DashApp

    admin_token = (
        getattr(args, "admin_token", None)
        or os.environ.get("RELAY_ADMIN_TOKEN")
        or profile.server().get("admin_token")
    )
    DashApp(args.scheduler, admin_token).run()
