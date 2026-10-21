from typing import ClassVar

import httpx
from rich.text import Text
from textual import work
from textual.app import ComposeResult
from textual.binding import BindingType
from textual.screen import Screen
from textual.widgets import DataTable, Footer, Header, Label

from relay import profile
from relay.commands import jobs as jobs_cmd
from relay.commands import nodes as nodes_cmd
from relay.commands.dash import JOB_COLUMNS, NODE_COLUMNS, REFRESH_S, human_ago

JOB_STYLES = {
    "queued": "yellow",
    "assigned": "blue",
    "running": "blue",
    "done": "green",
    "failed": "red",
}


class NodesScreen(Screen):
    CSS = """
    #summary { padding: 0 1; height: 1; color: $text-muted; }
    #nodes-error { padding: 0 1; height: auto; color: $error; }
    DataTable { height: 1fr; }
    """

    BINDINGS: ClassVar[list[BindingType]] = [
        ("r", "refresh", "Refresh"),
        ("p", "pool", "Pool"),
        ("l", "logout", "Log out"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self.node_rows: list[dict] = []
        self.job_rows: list[dict] = []
        self.error: str | None = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        yield Label("", id="summary")
        yield Label("", id="nodes-error")
        yield DataTable(id="nodes", cursor_type="row", zebra_stripes=True)
        yield DataTable(id="jobs", cursor_type="row", zebra_stripes=True)
        yield Footer()

    def on_mount(self) -> None:
        self.query_one("#nodes", DataTable).add_columns(*NODE_COLUMNS)
        self.query_one("#jobs", DataTable).add_columns(*JOB_COLUMNS)
        self.refresh_data()
        self.set_interval(REFRESH_S, self.refresh_data)
        # Redraw every second so the SEEN ages keep ticking between fetches.
        self.set_interval(1.0, self.redraw)

    @work(exclusive=True, thread=True)
    def refresh_data(self) -> None:
        prof = profile.load()
        token = prof.get("pool_token", "")
        scheduler = prof.get("scheduler", self.app.scheduler)
        try:
            node_rows = nodes_cmd.fetch(scheduler, token)
            job_rows = jobs_cmd.fetch(scheduler, token)
            self.error = None
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            # Keep showing the last good data when a refresh fails.
            node_rows, job_rows = self.node_rows, self.job_rows
            self.error = f"{type(exc).__name__}: {exc}"

        old_nodes = {j["id"]: j.get("node_id", "") for j in self.job_rows}
        for job in job_rows:
            old, new = old_nodes.get(job["id"]), job.get("node_id", "")
            if old and new and old != new:
                self.app.call_from_thread(
                    self.app.notify, f"job {job['id']} moved to {new}"
                )

        self.node_rows, self.job_rows = node_rows, job_rows
        self.app.call_from_thread(self.redraw)

    def node_state(self, n: dict) -> str:
        # Newer servers send status; fall back to last_seen for older ones.
        return n.get("status") or nodes_cmd.status(n["last_seen"])

    def redraw(self) -> None:
        node_table = self.query_one("#nodes", DataTable)
        job_table = self.query_one("#jobs", DataTable)
        node_table.clear()
        job_table.clear()

        rows = sorted(
            self.node_rows,
            key=lambda n: (self.node_state(n) != "online", n["hostname"]),
        )
        online = cores = 0
        mem = 0.0
        for n in rows:
            state = self.node_state(n)
            if state == "online":
                online += 1
                cores += n.get("cpus", 0)
                mem += n.get("mem_bytes", 0) / 1e9
            node_table.add_row(
                n["hostname"],
                Text("● online", style="green")
                if state == "online"
                else Text("● offline", style="red"),
                f"{n['os']}/{n['arch']}",
                str(n.get("cpus", 0)),
                f"{n.get('mem_bytes', 0) / 1e9:.1f}GB",
                human_ago(nodes_cmd.seconds_ago(n["last_seen"])),
                n.get("pool_id", "") or "-",
            )

        for job in self.job_rows:
            state = job["status"]
            job_table.add_row(
                job["id"],
                Text(state, style=JOB_STYLES.get(state, "")),
                job.get("node_id", "") or "-",
                str(job.get("attempts", 0)),
                jobs_cmd.age(job["created_at"]),
                jobs_cmd.shorten(" ".join(job["command"])),
            )

        pool_name = profile.load().get("pool_name", "")
        self.query_one("#summary", Label).update(
            f"{pool_name}   {online} online / {len(rows) - online} offline   "
            f"{cores} cores   {mem:.0f}GB   refreshed every {REFRESH_S:g}s"
        )
        self.query_one("#nodes-error", Label).update(self.error or "")

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_pool(self) -> None:
        self.app.show_pool()

    def action_logout(self) -> None:
        self.app.logout()
