
import getpass

import httpx
from textual import on, work
from textual.app import ComposeResult
from textual.containers import Vertical
from textual.screen import Screen
from textual.widgets import Footer, Header, Input, Label, Static

from relay.commands import auth
from relay.dash.app import parse_invite


class LoginScreen(Screen):
    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        with Vertical(classes="box"):
            yield Static("LOG IN", classes="heading")
            yield Static(
                "Paste the token that the Admin provided "
                "(the relay-agent line) or just the join token.",
                classes="hint",
            )
            yield Input(
                placeholder="relay-agent --scheduler ... --pool-token ...", id="invite"
            )
            yield Label("", id="login-error", classes="error")
        yield Footer()

    def on_mount(self) -> None:
        self.query_one("#invite", Input).focus()

    @on(Input.Submitted, "#invite")
    def on_invite(self, event: Input.Submitted) -> None:
        error = self.query_one("#login-error", Label)
        try:
            scheduler, token = parse_invite(event.value, self.app.scheduler)
        except ValueError as exc:
            error.update(str(exc))
            return
        error.update("logging in…")
        self.do_login(scheduler, token)

    @work(exclusive=True, thread=True)
    def do_login(self, scheduler: str, token: str) -> None:
        try:
            prof = auth.login(scheduler, token, getpass.getuser())
        except httpx.HTTPStatusError as exc:
            msg = (
                "invalid pool token"
                if exc.response.status_code in (401, 403)
                else f"login failed: {exc.response.status_code}"
            )
            self.app.call_from_thread(self.query_one("#login-error", Label).update, msg)
            return
        except (httpx.HTTPError, httpx.InvalidURL) as exc:
            self.app.call_from_thread(
                self.query_one("#login-error", Label).update, f"login failed: {exc}"
            )
            return
        self.app.call_from_thread(self.logged_in, prof)

    def logged_in(self, prof: dict) -> None:
        self.app.scheduler = prof["scheduler"]
        self.app.sub_title = prof["scheduler"]
        self.app.show_pool()