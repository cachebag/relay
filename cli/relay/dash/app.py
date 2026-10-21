"""One screen: who you are, the invite to share, the machines, the jobs."""

import shlex
from typing import ClassVar

import httpx
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Horizontal
from textual.widgets import Footer, Label, Static

from relay import profile
from relay.commands import jobs as jobs_cmd
from relay.commands import nodes as nodes_cmd
from relay.dash import theme
from relay.dash.modals import JoinModal, NewPoolModal
from relay.dash.tables import REFRESH_S, JobsTable, NodesTable, totals

AGENT = "relay-agent"


def invite_line(scheduler: str, token: str) -> str:
    return f"{AGENT} --scheduler {scheduler} --pool-token {token}"


def parse_invite(text: str, default_scheduler: str) -> tuple[str, str]:
    """Pull the scheduler and token out of whatever was pasted.

    Accepts a bare token, the full relay-agent line, or --flag=value forms.
    A `<your address>` placeholder from `relay pool create` on localhost is
    ignored in favour of the scheduler the dashboard is already using.
    """
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
        raise ValueError("no join token in that")
    if "<" in scheduler:
        scheduler = default_scheduler
    return scheduler.rstrip("/"), token


class DashApp(App[None]):
    TITLE = "relay"
    CSS = theme.CSS
    ENABLE_COMMAND_PALETTE = False

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("c", "copy_invite", "Copy invite"),
        Binding("n", "new_pool", "New pool"),
        Binding("r", "refresh", "Refresh"),
        Binding("l", "logout", "Log out"),
        Binding("q", "quit", "Quit"),
    ]

    def __init__(self, scheduler: str, admin_token: str | None = None) -> None:
        super().__init__()
        self.scheduler = scheduler
        self.admin_token = admin_token
        self.prof: dict = {}
        self.node_rows: list[dict] = []
        self.job_rows: list[dict] = []
        self.error: str | None = None
        self.invite_text = ""
        self.register_theme(theme.RELAY)
        self.theme = "relay"

    def compose(self) -> ComposeResult:
        with Horizontal(id="topbar"):
            yield Static("relay", classes="wordmark")
            yield Static("", id="where", classes="where")
            yield Static("", id="totals", classes="totals")
        with Horizontal(id="invite-bar"):
            yield Static("", id="invite-key", classes="key")
            yield Static("", id="invite-line", classes="invite")
        yield Static("NODES", classes="title")
        yield NodesTable(id="nodes")
        yield Static(
            "no machines yet. Send the invite above; each one shows up here "
            "within a few seconds of running it.",
            id="nodes-empty",
            classes="empty",
        )
        yield Static("JOBS", classes="title")
        yield JobsTable(id="jobs")
        yield Static(
            "no jobs yet. From another terminal: relay submit -- <command>",
            id="jobs-empty",
            classes="empty",
        )
        yield Label("", id="error")
        yield Footer()

    def on_mount(self) -> None:
        self.set_interval(REFRESH_S, self.refresh_data)
        # Redraw every second so the SEEN and AGE columns keep ticking.
        self.set_interval(1.0, self.redraw)
        self.use_profile(profile.load())
        if not self.prof.get("pool_token"):
            self.push_screen(JoinModal(can_cancel=False), self.joined)

    # -- login state ------------------------------------------------------

    def use_profile(self, prof: dict) -> None:
        self.prof = prof
        if prof.get("scheduler"):
            self.scheduler = prof["scheduler"]
        self.node_rows, self.job_rows, self.error = [], [], None
        self.redraw()
        if prof.get("pool_token"):
            self.refresh_data()

    def joined(self, prof: dict | None) -> None:
        if prof:
            self.use_profile(prof)
        elif not self.prof.get("pool_token"):
            self.exit()

    # -- data -------------------------------------------------------------

    @work(exclusive=True, thread=True)
    def refresh_data(self) -> None:
        token = self.prof.get("pool_token")
        if not token:
            return
        try:
            node_rows = nodes_cmd.fetch(self.scheduler, token)
            job_rows = jobs_cmd.fetch(self.scheduler, token)
            error = None
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            # Keep showing the last good data when a refresh fails.
            node_rows, job_rows = self.node_rows, self.job_rows
            error = f"can't reach {self.scheduler}: {exc}"
        self.node_rows, self.job_rows, self.error = node_rows, job_rows, error
        self.call_from_thread(self.redraw)

    def redraw(self) -> None:
        prof = self.prof
        logged_in = bool(prof.get("pool_token"))
        if logged_in:
            where = f"{prof.get('pool_name', '?')}  ({prof.get('pool_id', '?')})  {self.scheduler}"
            invite = invite_line(self.scheduler, prof["pool_token"])
        else:
            where, invite = self.scheduler, ""
        self.query_one("#where", Static).update(where)
        self.query_one("#totals", Static).update(
            totals(self.node_rows) if logged_in else ""
        )
        self.invite_text = invite
        self.query_one("#invite-key", Static).update("invite" if logged_in else "")
        self.query_one("#invite-line", Static).update(invite)
        self.query_one("#nodes-empty").display = logged_in and not self.node_rows
        self.query_one("#jobs-empty").display = logged_in and not self.job_rows
        hostnames = {n["id"]: n["hostname"] for n in self.node_rows}
        self.query_one(NodesTable).show(self.node_rows)
        self.query_one(JobsTable).show(self.job_rows, hostnames)
        self.query_one("#error", Label).update(self.error or "")

    # -- actions ----------------------------------------------------------

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_copy_invite(self) -> None:
        if self.invite_text:
            self.copy_to_clipboard(self.invite_text)
            self.notify("invite copied")

    def action_new_pool(self) -> None:
        self.push_screen(NewPoolModal(can_cancel=True), self.joined)

    def action_logout(self) -> None:
        profile.delete()
        self.use_profile({})
        self.push_screen(JoinModal(can_cancel=False), self.joined)
