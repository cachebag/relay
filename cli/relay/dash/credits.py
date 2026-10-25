"""Who has earned what in this pool."""

from typing import ClassVar

import httpx
from textual import work
from textual.app import ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Vertical
from textual.screen import ModalScreen
from textual.widgets import DataTable, Static

from relay.commands import credits as credits_cmd


class CreditsModal(ModalScreen[None]):
    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("r", "refresh", "Refresh"),
        Binding("escape", "dismiss", "Close", priority=True),
    ]

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal"):
            yield Static("CREDITS", classes="title")
            yield Static(
                "A machine earns its owner 1 credit for every minute it is online. "
                "A job costs its submitter 1 credit per minute it ran, at least 1. "
                "Balances can lag by up to 10 minutes.",
                classes="hint",
            )
            yield DataTable(id="credits", cursor_type="none")
            yield Static("", id="credits-error", classes="error")
            yield Static("r refresh   esc close", classes="keys")

    def on_mount(self) -> None:
        self.query_one(DataTable).add_columns("", "ACCOUNT", "BALANCE")
        self.load()

    @work(exclusive=True, thread=True)
    def load(self) -> None:
        app = self.app
        prof = app.prof
        try:
            rows = credits_cmd.fetch(
                app.scheduler, prof.get("pool_id", ""), prof.get("pool_token", "")
            )
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            app.call_from_thread(
                self.query_one("#credits-error", Static).update,
                f"can't load credits: {exc}",
            )
            return
        app.call_from_thread(self.show, rows, prof.get("name", ""))

    def show(self, rows: list[dict], me: str) -> None:
        table = self.query_one(DataTable)
        table.clear()
        for row in rows:
            table.add_row(
                "*" if row["account"] == me else "",
                row["account"],
                str(row["balance"]),
            )
        self.query_one("#credits-error", Static).update(
            ""
            if rows
            else "no credits yet; they appear after a machine has been online a while"
        )

    def action_refresh(self) -> None:
        self.load()
