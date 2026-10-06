from typing import ClassVar

import httpx
from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Container, Horizontal, Vertical
from textual.widgets import Button, Footer, Static

from relay.commands.nodes import fetch, status

REFRESH_S = 5.0

TITLE_ART = r"""
          _
 _ __ ___| | __ _ _   _
| '__/ _ \ |/ _` | | | |
| | |  __/ | (_| | |_| |
|_|  \___|_|\__,_|\__, |
                  |___/
""".strip("\n")

TAGLINE = "turn idle laptops into a shared compute pool"

# (button id, label, enabled). Disabled entries are placeholders until the
# scheduler grows a login / signup API.
MENU: list[tuple[str, str, bool]] = [
    ("dash", "Dashboard", True),
    ("signin", "Sign in", False),
    ("signup", "Sign up", False),
    ("quit", "Quit", True),
]


class MenuItem(Button):
    """A full-width menu button, navigated with the arrow keys."""


class MainMenu(Vertical):
    def compose(self) -> ComposeResult:
        yield Static("MENU", classes="card-title")
        for item_id, label, enabled in MENU:
            btn = MenuItem(label, id=f"menu-{item_id}")
            btn.disabled = not enabled
            yield btn


class StatusCard(Vertical):
    """Live snapshot of the scheduler: reachable or not, and pool size."""

    def compose(self) -> ComposeResult:
        yield Static("POOL", classes="card-title")
        yield Static("connecting…", id="pool-state")
        yield Static("", id="pool-nodes")
        yield Static("", id="pool-cores")
        yield Static("", id="pool-mem")
        yield Static("", id="pool-error")

    def show(self, rows: list[dict], error: str | None) -> None:
        state = self.query_one("#pool-state", Static)
        err = self.query_one("#pool-error", Static)
        if error:
            state.update("[red]● scheduler unreachable[/]")
            err.update(error)
            return

        online = [n for n in rows if status(n["last_seen"]) == "online"]
        cores = sum(n.get("cpus", 0) for n in online)
        mem = sum(n.get("mem_bytes", 0) for n in online) / 1e9
        state.update("[green]● scheduler online[/]")
        self.query_one("#pool-nodes", Static).update(
            f"nodes   {len(online)} online / {len(rows) - len(online)} offline"
        )
        self.query_one("#pool-cores", Static).update(f"cores   {cores}")
        self.query_one("#pool-mem", Static).update(f"memory  {mem:.0f} GB")
        err.update("")


class HomeApp(App[str | None]):
    """Landing screen. Exits with the id of the chosen menu item."""

    CSS = """
    Screen { align: center middle; }

    #frame { width: 84; height: auto; border: heavy $primary; padding: 1 2; }

    #title-box { width: 100%; height: auto; border: round $accent; padding: 0 2; }
    #title { color: $accent; text-style: bold; width: 100%; content-align: center middle; }
    #tagline { color: $text-muted; width: 100%; text-align: center; }

    #body { height: auto; margin-top: 1; }
    MainMenu { width: 1fr; height: auto; padding: 0 1; border-right: solid $accent; }
    StatusCard { width: 2fr; height: auto; padding: 0 2; }
    .card-title { color: $accent; text-style: bold; margin-bottom: 1; }

    MenuItem { width: 100%; height: 3; margin-bottom: 1; }
    MenuItem:disabled { color: $text-muted; }
    #pool-error { color: $error; }
    """

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("up", "focus_previous", "Up"),
        Binding("down", "focus_next", "Down"),
        Binding("r", "refresh", "Refresh"),
        Binding("q", "quit", "Quit"),
    ]

    def __init__(self, scheduler: str) -> None:
        super().__init__()
        self.scheduler = scheduler

    def compose(self) -> ComposeResult:
        with Container(id="frame"):
            with Vertical(id="title-box"):
                yield Static(TITLE_ART, id="title")
                yield Static(TAGLINE, id="tagline")
            with Horizontal(id="body"):
                yield MainMenu()
                yield StatusCard()
        yield Footer()

    def on_mount(self) -> None:
        self.title = "relay"
        self.sub_title = self.scheduler
        self.query_one("#menu-dash", MenuItem).focus()
        self.refresh_pool()
        self.set_interval(REFRESH_S, self.refresh_pool)

    @work(exclusive=True, thread=True)
    def refresh_pool(self) -> None:
        try:
            rows = fetch(self.scheduler)
            error = None
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            rows = []
            error = f"{type(exc).__name__}: {exc}"
        self.call_from_thread(self.query_one(StatusCard).show, rows, error)

    def action_refresh(self) -> None:
        self.refresh_pool()

    @on(Button.Pressed, "MenuItem")
    def on_menu(self, event: Button.Pressed) -> None:
        choice = (event.button.id or "").removeprefix("menu-")
        self.exit(None if choice == "quit" else choice)


def register(sub) -> None:
    p = sub.add_parser("home", help="landing screen with menu and pool status")
    p.set_defaults(func=run)


def run(args) -> None:
    from relay.commands.dash import Dashboard

    # Bounce between home and dashboard until one of them quits outright.
    while HomeApp(args.scheduler).run() == "dash":
        if Dashboard(args.scheduler).run() != "home":
            break
