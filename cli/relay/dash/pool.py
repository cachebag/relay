from typing import ClassVar

import httpx
from textual import on, work
from textual.app import ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Vertical
from textual.screen import Screen
from textual.widgets import Footer, Header, Input, Label, Static

from relay import profile
from relay.commands import pool as pool_cmd
from relay.dash.app import invite_line


class PoolScreen(Screen):
    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("ctrl+n", "nodes", "Nodes & jobs", priority=True),
        Binding("ctrl+l", "logout", "Log out", priority=True),
    ]
    def compose(self) -> ComposeResult:
        prof = profile.load()
        scheduler = prof.get("scheduler", self.app.scheduler)
        token = prof.get("pool_token", "")

        yield Header(show_clock=True)
        with Vertical(classes="box", id="pool-info"):
            yield Static("POOL", classes="heading")
            yield Static(f"name:      {prof.get('pool_name', '-')}", id="pool-name")
            yield Static(f"id:        {prof.get('pool_id', '-')}", id="pool-id")
            yield Static(f"scheduler: {scheduler}", id="pool-scheduler")
            yield Static(
                "invite (share this; contributors run it, you paste it to log in):",
                classes="hint",
            )
            yield Input(invite_line(scheduler, token), id="pool-invite")
        with Vertical(classes="box", id="pool-create"):
            yield Static("CREATE POOL", classes="heading")
            yield Input(
                self.app.admin_token or "",
                placeholder="server admin token",
                password=True,
                id="admin-token",
                )
            yield Input(placeholder="new pool name, then enter", id="new-pool")
            yield Label("", id="create-result")
            yield Label("", id="create-error", classes="error")
        yield Footer()

    def on_mount(self) -> None:
        # The invite is in a read-only box so it can be selected and copied.
        self.query_one("#pool-invite", Input).read_only = True

    @on(Input.Submitted, "#new-pool")
    def on_new_pool(self, event: Input.Submitted) -> None:
        name = event.value.strip()
        admin_token = self.query_one("#admin-token", Input).value.strip()
        error = self.query_one("#create-error", Label)
        if not name:
            return
        if not admin_token:
            error.update(pool_cmd.ADMIN_TOKEN_HINT)
            return
        error.update("")
        event.input.value = ""
        self.create_pool(name, admin_token)

    @work(exclusive=True, thread=True)
    def create_pool(self, name: str, admin_token: str) -> None:
        try:
            p = pool_cmd.create(self.app.scheduler, name, admin_token)
        except httpx.HTTPStatusError as exc:
            msg = (
                "create failed: the server admin token was rejected"
                if exc.response.status_code == 401
                else f"create failed: {exc.response.status_code} {exc.response.text.strip()}"
            )
            self.app.call_from_thread(
                self.query_one("#create-error", Label).update, msg
            )
            return
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            self.app.call_from_thread(
                self.query_one("#create-error", Label).update, f"create failed: {exc}"
            )
            return
        self.app.call_from_thread(self.created, name, p)

    def created(self, name: str, p: dict) -> None:
        self.app.admin_token = self.query_one("#admin-token", Input).value.strip()
        self.query_one("#create-result", Label).update(
            f"created {name} ({p['id']})\n"
            f"invite: {invite_line(self.app.scheduler, p['token'])}\n"
            f"pool admin token: {p.get('admin_token', '-')}  (shown once, save it now)"
        )


    def action_nodes(self) -> None:
        self.app.show_nodes()

    def action_logout(self) -> None:
        self.app.logout()