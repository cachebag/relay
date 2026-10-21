import shlex
from typing import ClassVar

from textual.app import App
from textual.binding import BindingType

from relay import profile

AGENT = " relay-agent"

def invite_line(scheduler: str, token: str) -> str:
    return f"{AGENT} --scheduler {scheduler} --pool-token {token}"


def parse_invite(text: str, default_scheduler: str) -> tuple[str, str]:
    try:
        parts = shlex.split(text.strip(), posix=True)
    except ValueError:
        parts = text.split()

    scheduler = default_scheduler
    token = ""
    bare: list[str] = []
    i = 0
    while i < len(parts):
        part = parts[i]
        if part in ("--scheduler", "--pool-token") and i + 1 < len(parts):
            if part == "--scheduler":
                scheduler = parts[i + 1]
            else:
                token = parts[i + 1]
            i += 2
            continue
        if part.startswith("--scheduler="):
            scheduler = part.split("=", 1)[1]
        elif part.startswith("--pool-token="):
            token = part.split("=", 1)[1]
        elif not part.startswith("-") and part != AGENT:
            bare.append(part)
        i += 1

    if not token and len(bare) == 1:
        token = bare[0]
    if not token:
        raise ValueError("no pool token in that invite")
    return scheduler.rstrip("/"), token


class DashApp(App[None]):

    TITLE = "relay"

    CSS = """
    .box { height: auto; border: round $accent; padding: 0 1; margin: 0 1; }
    .heading { color: $accent; text-style: bold; }
    .hint { color: $text-muted; }
    .error { color: $error; height: auto; }
    """

    BINDINGS: ClassVar[list[BindingType]] = [
        ("q", "quit", "Quit"),
    ]

    def __init__(self, scheduler: str, admin_token: str | None = None) -> None:
        super().__init__()
        self.scheduler = scheduler
        self.admin_token = admin_token

    def on_mount(self) -> None:
        self.sub_title = self.scheduler
        if profile.load().get("pool_token"):
            self.show_pool()
        else:
            self.show_login()

    def show_login(self) -> None:
        from relay.dash.login import LoginScreen

        self._show(LoginScreen())

    def show_pool(self) -> None:
        from relay.dash.pool import PoolScreen

        self._show(PoolScreen())

    def show_nodes(self) -> None:
        from relay.dash.nodes import NodesScreen

        self._show(NodesScreen())

    def _show(self, screen) -> None:
        # The default screen stays at the bottom of the stack; everything
        # else replaces whatever is on top so screens never pile up.
        if len(self.screen_stack) > 1:
            self.switch_screen(screen)
        else:
            self.push_screen(screen)

    def logout(self) -> None:
        profile.delete()
        self.show_login()