"""The two things you do before the dashboard is useful: join a pool, or make one."""

import getpass
from typing import ClassVar

import httpx
from textual import on, work
from textual.app import ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Vertical
from textual.screen import ModalScreen
from textual.widgets import DataTable, Input, Label, Static

from relay import profile
from relay.commands import auth
from relay.commands import pool as pool_cmd


class JoinModal(ModalScreen[dict | str | None]):
    """Paste a join token. Dismisses with the saved profile, "new" to create
    a pool instead, or None on cancel."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("ctrl+n", "new_pool", "Create a pool instead", priority=True),
        Binding("escape", "cancel", "Cancel", priority=True),
    ]

    def __init__(self, can_cancel: bool) -> None:
        super().__init__()
        # With no saved login there is nothing to go back to, so esc quits.
        self.can_cancel = can_cancel

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal"):
            yield Static("JOIN A POOL", classes="title")
            yield Static(
                "Paste the join token your admin sent you. "
                "The whole relay-agent line works too.",
                classes="hint",
            )
            yield Input(placeholder="join token", id="invite")
            yield Label("", id="join-error", classes="error")
            yield Static(
                "enter join   ctrl+n create a pool instead   "
                + ("esc back" if self.can_cancel else "esc quit"),
                classes="keys",
                )

    def on_mount(self) -> None:
        self.query_one("#invite", Input).focus()

    @on(Input.Submitted, "#invite")
    def on_submit(self, event: Input.Submitted) -> None:
        from relay.dash.app import parse_invite

        error = self.query_one("#join-error", Label)
        try:
            scheduler, token = parse_invite(event.value, self.app.scheduler)
        except ValueError as exc:
            error.update(str(exc))
            return
        error.update("logging in…")
        self.login(scheduler, token)

    @work(exclusive=True, thread=True)
    def login(self, scheduler: str, token: str) -> None:
        try:
            prof = auth.login(scheduler, token, getpass.getuser())
        except httpx.HTTPStatusError as exc:
            msg = (
                "that token isn't valid for this scheduler"
                if exc.response.status_code in (401, 403, 404)
                else f"login failed: {exc.response.status_code}"
            )
            self.app.call_from_thread(self.query_one("#join-error", Label).update, msg)
            return
        except (httpx.HTTPError, httpx.InvalidURL) as exc:
            self.app.call_from_thread(
                self.query_one("#join-error", Label).update,
                f"can't reach {scheduler}: {exc}",
            )
            return
        self.app.call_from_thread(self.dismiss, prof)

    def action_new_pool(self) -> None:
        # Hand off through the app so its callback stays attached. A
        # switch_screen here would drop it, and the new pool's login would
        # be saved but never shown.
        self.dismiss("new")

    def action_cancel(self) -> None:
        if self.can_cancel:
            self.dismiss(None)
        else:
            self.app.exit()


class NewPoolModal(ModalScreen[dict | None]):
    """Create a pool with the server admin token, then log in to it."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("escape", "cancel", "Cancel", priority=True),
    ]

    def __init__(self, can_cancel: bool) -> None:
        super().__init__()
        self.can_cancel = can_cancel
        self.created: dict | None = None
        # The scheduler the pool goes on: this machine's, never the login's (#146).
        self.target = ""

    def copyable(self) -> str:
        from relay.dash.app import invite_line

        if not self.created:
            return ""
        return invite_line(self.target, self.created["token"])

    def scheduler_address(self) -> str:
        """Where to create the pool: this machine's scheduler, or the address
        typed in when this machine doesn't run one."""
        if self.target:
            return self.target
        typed = self.query_one("#address", Input).value.strip()
        if typed and "://" not in typed:
            typed = f"http://{typed}"
        return typed

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal"):
            yield Static("CREATE A POOL", classes="title")
            yield Static(
                "Needs the server admin token that relayd printed when it first "
                "started. Only the person running relayd has it.",
                classes="hint",
            )
            self.target = getattr(self.app, "own_scheduler", "") or ""
            if self.target:
                yield Static(
                    f"creating on {self.target}", id="create-on", classes="hint"
                )
            else:
                yield Static(pool_cmd.NO_SERVER_HINT, id="create-on", classes="hint")
                yield Static("scheduler address", classes="label")
                yield Input(placeholder="http://localhost:8080", id="address")
            yield Static("server admin token", classes="label")
            yield Input(
                self.app.admin_token or "",
                placeholder="paste it here",
                password=True,
                id="admin-token",
                )
            yield Static("pool name", classes="label")
            yield Input(placeholder="lab", id="pool-name")
            yield Label("", id="create-error", classes="error")
            yield Static("", id="create-result", classes="result")
            yield Static(
                "enter create   " + ("esc back" if self.can_cancel else "esc quit"),
                id="create-keys",
                classes="keys",
                )

    def on_mount(self) -> None:
        token = self.query_one("#admin-token", Input)
        (self.query_one("#pool-name", Input) if token.value else token).focus()
        if not self.target:
            self.query_one("#address", Input).focus()

    @on(Input.Submitted, "#address")
    def on_address(self) -> None:
        token = self.query_one("#admin-token", Input)
        (self.query_one("#pool-name", Input) if token.value else token).focus()

    @on(Input.Submitted, "#admin-token")
    def on_token(self) -> None:
        self.query_one("#pool-name", Input).focus()

    @on(Input.Submitted, "#pool-name")
    def on_name(self, event: Input.Submitted) -> None:
        if self.created:
            self.finish()
            return
        name = event.value.strip()
        admin_token = self.query_one("#admin-token", Input).value.strip()
        error = self.query_one("#create-error", Label)
        if not name:
            error.update("give the pool a name")
            return
        if not admin_token:
            error.update("the server admin token is needed to create a pool")
            return
        if not self.scheduler_address():
            error.update("enter the address of the scheduler to create the pool on")
            self.query_one("#address", Input).focus()
            return
        self.target = self.scheduler_address()
        error.update("creating…")
        self.create(name, admin_token)

    @work(exclusive=True, thread=True)
    def create(self, name: str, admin_token: str) -> None:
        try:
            p = pool_cmd.create(self.target, name, admin_token)
        except httpx.HTTPStatusError as exc:
            msg = (
                "the server admin token was rejected"
                if exc.response.status_code == 401
                else f"create failed: {exc.response.status_code} {exc.response.text.strip()}"
            )
            self.app.call_from_thread(
                self.query_one("#create-error", Label).update, msg
            )
            return
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            self.app.call_from_thread(
                self.query_one("#create-error", Label).update,
                f"can't reach {self.target}: {exc}",
            )
            return
        self.app.call_from_thread(self.show_created, name, admin_token, p)

    def show_created(self, name: str, admin_token: str, p: dict) -> None:
        from relay.dash.app import invite_line

        self.created = p
        self.app.admin_token = admin_token
        self.app.pool_admin_token = p.get("admin_token")
        self.query_one("#create-error", Label).update("")
        self.query_one("#create-result", Static).update(
            f"[b]created {name}[/b]  ({p['id']})\n\n"
            f"invite, send this to contributors:\n"
            f"[$accent]{invite_line(self.target, p['token'])}[/]\n\n"
            f"pool admin token, shown once, keep it to yourself:\n"
            f"[$accent]{p.get('admin_token', '-')}[/]"
        )
        self.query_one("#create-keys", Static).update(
            "enter open this pool   esc close"
        )
        self.query_one("#pool-name", Input).focus()

    @on(Input.Submitted, "#admin-token")
    def on_token_after_create(self) -> None:
        if self.created:
            self.finish()

    def finish(self) -> None:
        assert self.created is not None
        self.log_in(self.target, self.created["token"])

    @work(exclusive=True, thread=True)
    def log_in(self, scheduler: str, token: str) -> None:
        try:
            prof = auth.login(scheduler, token, getpass.getuser())
        except (httpx.HTTPError, httpx.InvalidURL) as exc:
            self.app.call_from_thread(
                self.query_one("#create-error", Label).update, f"login failed: {exc}"
            )
            return
        self.app.call_from_thread(self.dismiss, prof)

    def action_cancel(self) -> None:
        if self.can_cancel:
            self.dismiss(None)
        else:
            self.app.exit()


class PoolPicker(ModalScreen[dict | str | None]):
    """Switch between the pools you are logged in to. Dismisses with the
    chosen login, "add" to join another, or None."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("a", "add", "Join another pool"),
        Binding("escape", "dismiss", "Close", priority=True),
    ]

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal"):
            yield Static("YOUR POOLS", classes="title")
            yield Static(
                "enter switches to the highlighted pool. "
                "a joins another one with a new token.",
                classes="hint",
            )
            yield DataTable(id="pools", cursor_type="row")
            yield Static("enter switch   a join another   esc close", classes="keys")

    def on_mount(self) -> None:
        table = self.query_one(DataTable)
        table.add_columns("", "NAME", "ID", "SCHEDULER")
        current = self.app.prof.get("pool_id")
        for i, login in enumerate(profile.logins()):
            pool_id = login.get("pool_id", "")
            table.add_row(
                "*" if pool_id == current else "",
                login.get("pool_name", "?"),
                pool_id,
                login.get("scheduler", "?"),
                key=pool_id,
            )
            if pool_id == current:
                table.move_cursor(row=i, animate=False)
        table.focus()

    @on(DataTable.RowSelected)
    def on_pick(self, event: DataTable.RowSelected) -> None:
        pool_id = str(event.row_key.value)
        chosen = next(
            (l for l in profile.logins() if l.get("pool_id") == pool_id), None
        )
        self.dismiss(chosen)

    def action_add(self) -> None:
        self.dismiss("add")
