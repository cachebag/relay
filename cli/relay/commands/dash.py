import os
from typing import ClassVar

import httpx
from rich.text import Text
from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import BindingType
from textual.containers import Horizontal
from textual.widgets import DataTable, Footer, Header, Input, Label

from relay import profile
from relay.commands import jobs as jobs_cmd
from relay.commands import pool as pool_cmd
from relay.commands.nodes import fetch, seconds_ago, status

# How often the dashboard refreshes node and job information.
REFRESH_S = 2.0

# Column headings for the two dashboard tables.
NODE_COLUMNS = ("HOST", "STATUS", "OS/ARCH", "CPU", "MEM", "SEEN", "POOL")
JOB_COLUMNS = ("ID", "STATUS", "NODE", "ATTEMPTS", "AGE", "COMMAND")


def human_ago(s: int) -> str:
    """Convert a number of seconds into a short human-readable age."""
    if s < 60:
        return f"{s}s ago"
    if s < 3600:
        return f"{s // 60}m ago"
    if s < 86400:
        return f"{s // 3600}h ago"
    return f"{s // 86400}d ago"


class Dashboard(App):
    """Textual dashboard that displays relay nodes and jobs."""

    CSS = """
    #bar { height: auto; padding: 0 1; }
    #bar Input { width: 28; }
    #summary { padding: 0 1; height: 1; color: $text-muted; }
    #error { padding: 0 1; height: auto; color: $error; }
    DataTable { height: 1fr; }
    """

    # Keyboard shortcuts available while the dashboard is open.
    BINDINGS: ClassVar[list[BindingType]] = [
        ("r", "refresh", "Refresh"),
        ("escape", "back", "Home"),
        ("q", "quit", "Quit"),
    ]

    def __init__(
        self, scheduler: str, token: str, admin_token: str | None = None
    ) -> None:
        """Store the scheduler, the join token, and the optional server admin token."""
        super().__init__()
        self.scheduler = scheduler
        self.token = token
        self.admin_token = admin_token
        self.node_rows: list[dict] = []
        self.job_rows: list[dict] = []
        self.error: str | None = None

    def compose(self) -> ComposeResult:
        """Build the dashboard interface and its widgets."""
        yield Header(show_clock=True)

        with Horizontal(id="bar"):
            yield Label("new pool ")
            yield Input(placeholder="name", id="pool-name")

        yield Label("", id="summary")
        yield Label("", id="error")

        # Nodes are shown in the first table.
        yield DataTable(id="nodes", cursor_type="row", zebra_stripes=True)

        # Jobs are shown in a second table directly below the nodes table.
        yield DataTable(id="jobs", cursor_type="row", zebra_stripes=True)

        yield Footer()

    def on_mount(self) -> None:
        """Configure the tables and start the automatic refresh timers."""
        self.title = "relay"
        self.sub_title = self.scheduler

        # Add the proper columns to each table.
        self.query_one("#nodes", DataTable).add_columns(*NODE_COLUMNS)
        self.query_one("#jobs", DataTable).add_columns(*JOB_COLUMNS)

        # Load data immediately, then refresh it every REFRESH_S seconds.
        self.refresh_data()
        self.set_interval(REFRESH_S, self.refresh_data)

        # Redraw once per second so the displayed node ages continue updating.
        self.set_interval(1.0, self.redraw)

    @work(exclusive=True, thread=True)
    def refresh_data(self) -> None:
        """Fetch the latest nodes and jobs and detect job node changes."""
        try:
            # The join token decides which pool's nodes and jobs we see.
            node_rows = fetch(self.scheduler, self.token)
            job_rows = jobs_cmd.fetch(self.scheduler, self.token)
            self.error = None
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            # Keep displaying the previous data if a refresh fails.
            node_rows = self.node_rows
            job_rows = self.job_rows
            self.error = f"{type(exc).__name__}: {exc}"

        # Remember which node each previously displayed job was assigned to.
        old_nodes = {job["id"]: job.get("node_id", "") for job in self.job_rows}

        # Notify the user when an existing job moves from one node to another.
        for job in job_rows:
            job_id = job["id"]
            new_node = job.get("node_id", "")
            old_node = old_nodes.get(job_id)

            if old_node and new_node and old_node != new_node:
                self.call_from_thread(
                    self.notify,
                    f"job {job_id} moved to {new_node}",
                )

        # Save the newly fetched data for the next redraw.
        self.node_rows = node_rows
        self.job_rows = job_rows

        # UI updates must be sent back to Textual's main thread.
        self.call_from_thread(self.redraw)

    @work(exclusive=True, thread=True)
    def create_pool(self, name: str) -> None:
        """Create a pool, show its ID/token, and refresh the dashboard."""
        if not self.admin_token:
            self.error = pool_cmd.ADMIN_TOKEN_HINT
            self.call_from_thread(self.redraw)
            return
        try:
            p = pool_cmd.create(self.scheduler, name, self.admin_token)
            self.error = None

            self.call_from_thread(
                self.notify,
                f"pool id: {p['id']}\njoin token: {p['token']}",
                title=f"created {name}",
                timeout=30,
            )
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            self.error = f"create failed - {type(exc).__name__}: {exc}"

        # Refresh nodes and jobs after attempting to create the pool.
        self.refresh_data()

    def redraw(self) -> None:
        """Clear and repopulate both dashboard tables from cached data."""
        node_table = self.query_one("#nodes", DataTable)
        job_table = self.query_one("#jobs", DataTable)

        node_table.clear()
        job_table.clear()

        # Keep online nodes first, then sort nodes alphabetically by hostname.
        rows = sorted(
            self.node_rows,
            key=lambda n: (
                (n.get("status") or status(n["last_seen"])) != "online",
                n["hostname"],
            ),
        )

        online = 0
        cores = 0
        mem = 0.0

        # Populate the nodes table and calculate summary totals.
        for n in rows:
            secs = seconds_ago(n["last_seen"])

            # Prefer server-provided status, but support older servers by
            # calculating status from last_seen when status is not present.
            state = n.get("status") or status(n["last_seen"])

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
                human_ago(secs),
                n.get("pool_id", "") or "-",
            )

        # Populate the jobs table.
        for job in self.job_rows:
            state = job["status"]

            # Give each job state the color required by I43.
            styles = {
                "queued": "yellow",
                "assigned": "blue",
                "running": "blue",
                "done": "green",
                "failed": "red",
            }

            status_text = Text(state, style=styles.get(state, ""))

            # The API stores commands as a list of strings, so join them for
            # display before applying the same 40-character truncation used
            # by the `relay jobs` command.
            command = " ".join(job["command"])

            job_table.add_row(
                job["id"],
                status_text,
                job.get("node_id", "") or "-",
                str(job.get("attempts", 0)),
                jobs_cmd.age(job["created_at"]),
                jobs_cmd.shorten(command),
            )

        # Update the summary and any current error message.
        self.query_one("#summary", Label).update(
            f"{online} online / {len(rows) - online} offline   "
            f"{cores} cores   {mem:.0f}GB   refreshed every {REFRESH_S:g}s"
        )
        self.query_one("#error", Label).update(self.error or "")

    @on(Input.Submitted, "#pool-name")
    def on_pool_submitted(self, event: Input.Submitted) -> None:
        """Create a pool when the user submits a name in the pool input."""
        name = event.value.strip()

        if name:
            event.input.value = ""
            self.create_pool(name)

    def action_refresh(self) -> None:
        """Handle the manual refresh keyboard action."""
        self.refresh_data()

    def action_back(self) -> None:
        """Exit with "home" so a caller that chains screens can return to it."""
        self.exit("home")


def register(sub) -> None:
    """Register the `relay dash` command with the CLI argument parser."""
    p = sub.add_parser("dash", help="live dashboard of your pool")
    p.add_argument(
        "--admin-token",
        help="server admin token, only needed to create pools (defaults to $RELAY_ADMIN_TOKEN)",
    )
    p.set_defaults(func=run_app)


def run(args) -> None:
    """Launch the Textual dashboard for the pool you are logged in to."""
    prof = profile.require_login()
    admin_token = args.admin_token or os.environ.get("RELAY_ADMIN_TOKEN")
    Dashboard(args.scheduler, prof["pool_token"], admin_token).run()



def run_app(args) -> None:
    from relay.dash import DashApp
    admin_token = args.admin_token or os.environ.get("RELAY_ADMIN_TOKEN")
    DashApp(args.scheduler, admin_token).run()