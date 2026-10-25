"""Admin actions that change things for everyone, so they ask twice:
the pool admin token, and the pool's name typed out."""

from collections.abc import Callable
from typing import ClassVar

import httpx
from textual import on, work
from textual.app import ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Vertical
from textual.screen import ModalScreen
from textual.widgets import Input, Label, Static


class AdminModal(ModalScreen[object | None]):
    """`run(admin_token)` does the work in a thread and returns a result.
    The modal shows `describe(result)` and dismisses with the result when
    closed, or None if nothing happened."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("escape", "close", "Close", priority=True),
    ]

    def __init__(
        self,
        title: str,
        hint: str,
        run: Callable[[str], object],
        describe: Callable[[object], str],
        copy: Callable[[object], str] | None = None,
    ) -> None:
        super().__init__()
        self.title_text = title
        self.hint = hint
        self.run = run
        self.describe = describe
        self.copy = copy
        self.result: object | None = None

    def copyable(self) -> str:
        """What ctrl+c copies once the action has run."""
        if self.result is None or self.copy is None:
            return ""
        return self.copy(self.result)

    def compose(self) -> ComposeResult:
        pool_name = self.app.prof.get("pool_name", "")
        with Vertical(classes="modal"):
            yield Static(self.title_text, classes="title")
            yield Static(self.hint, classes="hint")
            yield Static("pool admin token", classes="label")
            yield Input(
                self.app.pool_admin_token or "",
                placeholder="the pool's admin token, printed when it was created",
                password=True,
                id="admin-token",
            )
            yield Static(
                f"type the pool name ({pool_name}) to confirm", classes="label"
            )
            yield Input(placeholder=pool_name, id="confirm-name")
            yield Label("", id="admin-error", classes="error")
            yield Static("", id="admin-result", classes="result")
            yield Static("enter confirm   esc back", id="admin-keys", classes="keys")

    def on_mount(self) -> None:
        token = self.query_one("#admin-token", Input)
        (self.query_one("#confirm-name", Input) if token.value else token).focus()

    @on(Input.Submitted, "#admin-token")
    def on_token(self) -> None:
        if self.result is None:
            self.query_one("#confirm-name", Input).focus()
        else:
            self.action_close()

    @on(Input.Submitted, "#confirm-name")
    def on_name(self, event: Input.Submitted) -> None:
        if self.result is not None:
            self.action_close()
            return
        error = self.query_one("#admin-error", Label)
        admin_token = self.query_one("#admin-token", Input).value.strip()
        if not admin_token:
            error.update("the pool admin token is needed for this")
            self.query_one("#admin-token", Input).focus()
            return
        if event.value.strip() != self.app.prof.get("pool_name", ""):
            error.update("that isn't this pool's name; nothing was changed")
            return
        error.update("working…")
        self.do(admin_token)

    @work(exclusive=True, thread=True)
    def do(self, admin_token: str) -> None:
        try:
            result = self.run(admin_token)
        except httpx.HTTPStatusError as exc:
            code = exc.response.status_code
            msg = (
                "the admin token was rejected"
                if code == 401
                else f"failed: {code} {exc.response.text.strip()}"
            )
            self.app.call_from_thread(self.query_one("#admin-error", Label).update, msg)
            return
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            self.app.call_from_thread(
                self.query_one("#admin-error", Label).update, f"failed: {exc}"
            )
            return
        self.app.call_from_thread(self.done, admin_token, result)

    def done(self, admin_token: str, result: object) -> None:
        self.result = result
        self.app.pool_admin_token = admin_token
        self.query_one("#admin-error", Label).update("")
        self.query_one("#admin-result", Static).update(self.describe(result))
        self.query_one("#admin-keys", Static).update("esc close")

    def action_close(self) -> None:
        self.dismiss(self.result)
