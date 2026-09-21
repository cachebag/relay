from typing import ClassVar

import httpx
from rich.text import Text
from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import BindingType
from textual.containers import Horizontal
from textual.widgets import DataTable, Footer, Header, Input, Label

from relay.commands import pool as pool_cmd
from relay.commands.nodes import fetch, seconds_ago, status

REFRESH_S = 2.0

COLUMNS = ("HOST", "STATUS", "OS/ARCH", "CPU", "MEM", "SEEN", "POOL")


def human_ago(s: int) -> str:
    if s < 60:
        return f"{s}s ago"
    if s < 3600:
        return f"{s // 60}m ago"
    if s < 86400:
        return f"{s // 3600}h ago"
    return f"{s // 86400}d ago"


class Dashboard(App):
    CSS = """
    #bar { height: auto; padding: 0 1; }
    #bar Input { width: 28; }
    #summary { padding: 0 1; height: 1; color: $text-muted; }
    #error { padding: 0 1; height: auto; color: $error; }
    DataTable { height: 1fr; }
    """

    BINDINGS: ClassVar[list[BindingType]] = [
        ("r", "refresh", "Refresh"),
        ("q", "quit", "Quit"),
    ]

    def __init__(self, scheduler: str, pool: str | None = None) -> None:
        super().__init__()
        self.scheduler = scheduler
        self.pool = pool
        self.rows: list[dict] = []
        self.error: str | None = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        with Horizontal(id="bar"):
            yield Label("new pool ")
            yield Input(placeholder="name", id="pool-name")
        yield Label("", id="summary")
        yield Label("", id="error")
        yield DataTable(id="nodes", cursor_type="row", zebra_stripes=True)
        yield Footer()

    def on_mount(self) -> None:
        self.title = "relay"
        self.sub_title = self.scheduler + (f"  --pool {self.pool}" if self.pool else "")
        self.query_one(DataTable).add_columns(*COLUMNS)
        self.refresh_nodes()
        self.set_interval(REFRESH_S, self.refresh_nodes)
        self.set_interval(1.0, self.redraw)

    @work(exclusive=True, thread=True)
    def refresh_nodes(self) -> None:
        try:
            rows = fetch(self.scheduler, self.pool)
            self.error = None
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            rows = self.rows
            self.error = f"{type(exc).__name__}: {exc}"
        self.rows = rows
        self.call_from_thread(self.redraw)

    @work(exclusive=True, thread=True)
    def create_pool(self, name: str) -> None:
        try:
            p = pool_cmd.create(self.scheduler, name)
            self.error = None
            self.call_from_thread(
                self.notify,
                f"pool id: {p['id']}\njoin token: {p['token']}",
                title=f"created {name}",
                timeout=30,
            )
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            self.error = f"create failed - {type(exc).__name__}: {exc}"
        self.refresh_nodes()

    def redraw(self) -> None:
        table = self.query_one(DataTable)
        table.clear()

        rows = sorted(
            self.rows,
            key=lambda n: (status(n["last_seen"]) != "online", n["hostname"]),
        )

        online = 0
        cores = 0
        mem = 0.0
        for n in rows:
            secs = seconds_ago(n["last_seen"])
            state = status(n["last_seen"])
            if state == "online":
                online += 1
                cores += n.get("cpus", 0)
                mem += n.get("mem_bytes", 0) / 1e9
            table.add_row(
                n["hostname"],
                Text("● online", style="green")
                if state == "online"
                else Text("● offline", style="red"),
                f"{n['os']}/{n['arch']}",
                str(n.get("cpus", 0)),
                f"{n.get('mem_bytes', 0) / 1e9:.1f}GB",
                human_ago(secs),
                n.get("pool_id", "") or "-",
                )

        self.query_one("#summary", Label).update(
            f"{online} online / {len(rows) - online} offline   "
            f"{cores} cores   {mem:.0f}GB   refreshed every {REFRESH_S:g}s"
        )
        self.query_one("#error", Label).update(self.error or "")

    @on(Input.Submitted, "#pool-name")
    def on_pool_submitted(self, event: Input.Submitted) -> None:
        name = event.value.strip()
        if name:
            event.input.value = ""
            self.create_pool(name)

    def action_refresh(self) -> None:
        self.refresh_nodes()


def register(sub) -> None:
    p = sub.add_parser("dash", help="live dashboard of nodes in a pool")
    p.add_argument("--pool", help="only show nodes in this pool id")
    p.set_defaults(func=run)


def run(args) -> None:
    Dashboard(args.scheduler, args.pool).run()