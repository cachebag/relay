"""One screen: who you are, the invite to share, the machines, the jobs."""

import shlex
from typing import ClassVar

import httpx
from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Horizontal, Vertical
from textual.widgets import Footer, Input, Label, Static

# Share agent lifecycle logic with the `relay contribute` CLI command.
from relay import contribution, profile
from relay.commands import jobs as jobs_cmd
from relay.commands import node as node_cmd
from relay.commands import nodes as nodes_cmd
from relay.commands import pool as pool_cmd
from relay.commands import submit as submit_cmd
from relay.dash import theme
from relay.dash.admin import AdminModal
from relay.dash.credits import CreditsModal
from relay.dash.job import JobModal
from relay.dash.modals import JoinModal, NewPoolModal, PoolPicker
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


class SubmitInput(Input):
    """The command box. Esc hands the keyboard back to the tables."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("escape", "leave", "Back to jobs"),
    ]

    def action_leave(self) -> None:
        self.app.query_one(JobsTable).focus()


class DashApp(App[None]):
    TITLE = "relay"
    CSS = theme.CSS
    ENABLE_COMMAND_PALETTE = False

    BINDINGS: ClassVar[list[BindingType]] = [
        # A starts or stops the agent on this machine.
        Binding("a", "toggle_contribution", "Contribute"),
        Binding("s", "submit", "Submit"),
        Binding("k", "credits", "Credits"),
        Binding("t", "rotate_token", "Rotate token"),
        Binding("x", "remove_node", "Remove node"),
        Binding("c", "copy_invite", "Copy invite"),
        # Mouse drags go to the dashboard, not the terminal, so Cmd+C has
        # nothing to copy. Ctrl+C copies the selection, or the thing under
        # the cursor, through the terminal's clipboard escape.
        Binding("ctrl+c", "copy_text", "Copy", show=False, priority=True),
        Binding("n", "new_pool", "New pool"),
        Binding("p", "switch_pool", "Pools"),
        Binding("r", "refresh", "Refresh", show=False),
        Binding("l", "logout", "Log out"),
        Binding("q", "quit", "Quit"),
    ]

    def __init__(
        self,
        scheduler: str,
        admin_token: str | None = None,
        scheduler_given: bool = True,
    ) -> None:
        super().__init__()
        self.scheduler = scheduler
        self.own_scheduler = scheduler if scheduler_given else pool_cmd.own_scheduler()
        self.admin_token = admin_token
        self.pool_admin_token: str | None = None
        self.prof: dict = {}
        self.node_rows: list[dict] = []
        self.job_rows: list[dict] = []
        self.error: str | None = None
        self.invite_text = ""
        # Prevent overlapping start/stop requests while the worker is active.
        self._contribution_busy = False
        self._contribution_action = ""
        self.register_theme(theme.RELAY)
        self.theme = "relay"

    def compose(self) -> ComposeResult:
        with Horizontal(id="topbar"):
            yield Static("relay", classes="wordmark")
            yield Static("", id="where", classes="where")
            yield Static("", id="totals", classes="totals")
            # Local agent state is separate from the scheduler's node totals.
            yield Static("", id="contribution", classes="contribution")
        with Horizontal(id="invite-bar"):
            yield Static("", id="invite-key", classes="key")
            yield Static("", id="invite-line", classes="invite")
        with Vertical(id="body"):
            yield Static("NODES", classes="title")
            yield NodesTable(id="nodes")
            yield Static(
                "no machines yet. Send the invite above; each one shows up here "
                "within a few seconds of running it.",
                id="nodes-empty",
                classes="empty",
            )
            yield Static("JOBS", classes="title")
            yield Static(
                "no jobs yet. Press s, type a command, enter.",
                id="jobs-empty",
                classes="empty",
            )
            # Let the jobs table use the remaining space and scroll internally.
            yield JobsTable(id="jobs")
            yield Label("", id="error")
        with Horizontal(id="submit-bar"):
            yield Static("run", classes="key")
            yield SubmitInput(
                placeholder="a command to run on the pool, then enter", id="submit"
            )
        yield Footer()

    def on_mount(self) -> None:
        self.set_interval(REFRESH_S, self.refresh_data)
        # Redraw every second so the SEEN and AGE columns keep ticking.
        self.set_interval(1.0, self.redraw)
        self.use_profile(profile.load())
        self.query_one(JobsTable).focus()
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

    def joined(self, prof: dict | str | None) -> None:
        logged_in = bool(self.prof.get("pool_token"))
        # The join dialog returns "new" when a user chooses to create a pool.
        if prof == "new":
            self.push_screen(NewPoolModal(can_cancel=logged_in), self.joined)
        elif isinstance(prof, dict) and prof:
            self.use_profile(prof)
        elif not logged_in:
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
        # The 1 s timer and the refresh worker can both land after the app has
        # started shutting down and the screen is gone; nothing to draw then.
        if not self.query("#where"):
            return
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
        self.redraw_contribution()
        self.invite_text = invite
        self.query_one("#invite-key", Static).update("invite" if logged_in else "")
        self.query_one("#invite-line", Static).update(invite)
        self.query_one("#nodes-empty").display = logged_in and not self.node_rows
        self.query_one("#jobs-empty").display = logged_in and not self.job_rows
        self.query_one("#submit-bar").display = logged_in
        hostnames = {n["id"]: n["hostname"] for n in self.node_rows}
        self.query_one(NodesTable).show(self.node_rows)
        # Show the newest jobs first while retaining all rows for scrolling.
        newest_first = sorted(
            self.job_rows, key=lambda job: job.get("created_at", ""), reverse=True
        )
        self.query_one(JobsTable).show(newest_first, hostnames)
        self.query_one("#error", Label).update(self.error or "")

    def redraw_contribution(self) -> None:
        """Update the dashboard indicator from the locally tracked agent."""
        indicator = self.query_one("#contribution", Static)

        if self._contribution_busy:
            # Starting the agent can take several seconds.
            indicator.update(f"this machine: {self._contribution_action}...")
            indicator.set_class(False, "running")
            return

        try:
            running = contribution.status()
        except contribution.ContributionError, OSError:
            indicator.update("this machine: status unavailable")
            indicator.set_class(False, "running")
            return

        label = (
            "this machine: contributing"
            if running is not None
            else "this machine: not contributing"
        )
        indicator.update(label)
        indicator.set_class(running is not None, "running")

    # -- actions ----------------------------------------------------------

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_toggle_contribution(self) -> None:
        """Toggle the local agent without blocking the dashboard UI."""
        if self._contribution_busy:
            return

        try:
            running = contribution.status()
        except (contribution.ContributionError, OSError) as exc:
            self.notify(str(exc), severity="error")
            return

        if running is None and not self.prof.get("pool_token"):
            self.notify("not logged in, run relay login first", severity="warning")
            return

        # Capture credentials now in case the user switches pools later.
        self._contribution_busy = True
        self._contribution_action = "stopping" if running else "starting"
        self.redraw_contribution()
        self.toggle_contribution(running is not None, self.prof.copy(), self.scheduler)

    # Keep agent operations in their own worker group so periodic dashboard
    # refreshes cannot cancel a pending startup or shutdown.
    @work(exclusive=True, thread=True, group="contribution")
    def toggle_contribution(
        self, was_running: bool, prof: dict, scheduler: str
    ) -> None:
        """Start or stop through the same functions used by the CLI."""
        try:
            if was_running:
                contribution.stop()
                message = "stopped contributing"
            else:
                contribution.start(prof, scheduler)
                message = f"contributing to {prof.get('pool_name', 'your pool')}"
        except (contribution.ContributionError, OSError) as exc:
            # Textual widgets must be updated on the main UI thread.
            self.call_from_thread(self.finish_contribution, str(exc), True)
            return

        self.call_from_thread(self.finish_contribution, message, False)

    def finish_contribution(self, message: str, failed: bool) -> None:
        """Re-enable the toggle and refresh the UI when the worker finishes."""
        self._contribution_busy = False
        self._contribution_action = ""
        self.redraw_contribution()
        self.notify(message, severity="error" if failed else "information")
        self.refresh_data()

    def action_copy_invite(self) -> None:
        if self.invite_text:
            self.copy_to_clipboard(self.invite_text)
            self.notify("invite copied")

    def action_copy_text(self) -> None:
        focused = self.focused
        text = (
            getattr(focused, "selected_text", "") if isinstance(focused, Input) else ""
        )
        text = text or self.screen.get_selected_text() or ""
        if not text:
            getter = getattr(self.screen, "copyable", None)
            text = getter() if getter else self.copyable()
        if text:
            self.copy_to_clipboard(text)
            self.notify("copied")

    def copyable(self) -> str:
        """What ctrl+c copies on the main screen with nothing selected."""
        focused = self.focused
        if isinstance(focused, (JobsTable, NodesTable)):
            return focused.highlighted_key() or ""
        return self.invite_text

    def action_new_pool(self) -> None:
        self.push_screen(NewPoolModal(can_cancel=True), self.joined)

    def action_logout(self) -> None:
        left = self.prof.get("pool_name", "")
        remaining = profile.delete()
        self.pool_admin_token = None
        if remaining:
            # Logged in elsewhere too, so land there instead of the join box.
            self.use_profile(remaining)
            self.notify(
                f"logged out of {left}, now in {remaining.get('pool_name', '?')}"
            )
            return
        self.use_profile({})
        self.push_screen(JoinModal(can_cancel=False), self.joined)

    def action_switch_pool(self) -> None:
        if profile.logins():
            self.push_screen(PoolPicker(), self.picked)

    def picked(self, choice: dict | str | None) -> None:
        if choice == "add":
            self.push_screen(JoinModal(can_cancel=True), self.joined)
        elif isinstance(choice, dict) and choice.get("pool_id") != self.prof.get(
            "pool_id"
        ):
            self.pool_admin_token = None
            self.use_profile(profile.use(choice["pool_id"]))

    # -- submit -----------------------------------------------------------

    def action_submit(self) -> None:
        if self.prof.get("pool_token"):
            self.query_one("#submit", Input).focus()

    @on(Input.Submitted, "#submit")
    def on_submit(self, event: Input.Submitted) -> None:
        try:
            command = shlex.split(event.value)
        except ValueError as exc:
            self.error = f"can't parse that command: {exc}"
            self.redraw()
            return
        if not command:
            return
        event.input.value = ""
        self.submit(command)

    @work(thread=True)
    def submit(self, command: list[str]) -> None:
        prof = self.prof
        try:
            job = submit_cmd.submit(
                self.scheduler, command, prof["pool_token"], prof.get("name", "")
            )
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            self.error = f"submit failed: {exc}"
            self.call_from_thread(self.redraw)
            return
        self.call_from_thread(self.notify, f"submitted {job['id']}")
        self.refresh_data()

    # -- job output -------------------------------------------------------

    def hostnames(self) -> dict[str, str]:
        return {n["id"]: n["hostname"] for n in self.node_rows}

    @on(JobsTable.RowSelected, "#jobs")
    def on_job_selected(self, event: JobsTable.RowSelected) -> None:
        self.push_screen(JobModal(str(event.row_key.value), self.hostnames()))

    # -- credits and admin ------------------------------------------------

    def action_credits(self) -> None:
        if self.prof.get("pool_token"):
            self.push_screen(CreditsModal())

    def action_rotate_token(self) -> None:
        if not self.prof.get("pool_token"):
            return
        pool_id = self.prof.get("pool_id", "")

        def run(admin_token: str) -> str:
            return pool_cmd.rotate_token(self.scheduler, pool_id, admin_token)

        def describe(new_token: object) -> str:
            return (
                "[b]rotated[/b]. The old join token no longer works; every "
                "contributor restarts their agent with the new invite:\n"
                f"[$accent]{invite_line(self.scheduler, str(new_token))}[/]\n\n"
                "This dashboard switched to the new token already."
            )

        self.push_screen(
            AdminModal(
                "ROTATE THE JOIN TOKEN",
                "Everyone's current invite stops working. Use this if a token "
                "leaked or you want to drop everyone who has it.",
                run,
                describe,
                copy=lambda t: invite_line(self.scheduler, str(t)),
            ),
            self.rotated,
        )

    def rotated(self, new_token: object | None) -> None:
        if new_token:
            prof = {**self.prof, "pool_token": str(new_token)}
            profile.save(prof)
            self.use_profile(prof)

    def action_remove_node(self) -> None:
        table = self.query_one(NodesTable)
        if not self.prof.get("pool_token") or not table.row_count:
            return
        node_id = table.highlighted_key() or ""
        host = self.hostnames().get(node_id, node_id)
        pool_id = self.prof.get("pool_id", "")

        def run(admin_token: str) -> str:
            node_cmd.remove(self.scheduler, pool_id, node_id, admin_token)
            return node_id

        def describe(_: object) -> str:
            return (
                f"[b]removed {host}[/b]. Its unfinished jobs went back to the "
                "queue without using an attempt, and its agent stops on its "
                "next heartbeat."
            )

        self.push_screen(
            AdminModal(
                f"REMOVE {host}",
                f"Takes {host} ({node_id}) out of the pool. They can "
                "rejoin with a new invite granted the invite token had been rotated.",
                run,
                describe,
            ),
            lambda _: self.refresh_data(),
        )
