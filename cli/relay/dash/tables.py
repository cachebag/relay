"""The nodes and jobs tables. Each one takes the scheduler's rows and draws them."""

from typing import ClassVar

from rich.text import Text
from textual.binding import Binding, BindingType
from textual.widgets import DataTable

from relay.commands import jobs as jobs_cmd
from relay.commands import nodes as nodes_cmd
from relay.commands import status as status_cmd
from relay.dash.theme import JOB_STYLES, OFFLINE, ONLINE

# How often the dashboard asks the scheduler for nodes and jobs.
REFRESH_S = 2.0

NODE_COLUMNS = ("ID", "HOST", "STATUS", "OS/ARCH", "CPU", "MEM", "SEEN")
JOB_COLUMNS = ("ID", "STATUS", "NODE", "ATTEMPTS", "AGE", "COMMAND")


def human_ago(s: int) -> str:
    if s < 60:
        return f"{s}s ago"
    if s < 3600:
        return f"{s // 60}m ago"
    if s < 86400:
        return f"{s // 3600}h ago"
    return f"{s // 86400}d ago"


def node_state(n: dict) -> str:
    # Newer servers send status; fall back to last_seen for older ones.
    return n.get("status") or nodes_cmd.status(n["last_seen"])


class LiveTable(DataTable):
    """A table redrawn every second that doesn't lose your place.

    When the same rows are still there, cells are updated in place. When
    rows came or went, the table is rebuilt and the cursor is put back on
    the row it was on."""

    COLUMNS: tuple[str, ...] = ()

    def on_mount(self) -> None:
        self.cursor_type = "row"
        # The cursor row keeps each cell's own colour (green online, red failed).
        self.cursor_foreground_priority = "renderable"
        self.column_keys = self.add_columns(*self.COLUMNS)

    def highlighted_key(self) -> str | None:
        if not self.row_count or self.cursor_row >= self.row_count:
            return None
        return str(self.coordinate_to_cell_key(self.cursor_coordinate).row_key.value)

    def replace(self, rows: list[tuple[str, tuple]]) -> None:
        keys = [key for key, _ in rows]
        if keys == [row_key.value for row_key in self.rows]:
            for key, cells in rows:
                for column, value in zip(self.column_keys, cells, strict=True):
                    self.update_cell(key, column, value, update_width=True)
            return
        was_on = self.highlighted_key()
        self.clear()
        for key, cells in rows:
            self.add_row(*cells, key=key)
        if was_on in keys:
            self.move_cursor(row=keys.index(was_on), animate=False)


class NodesTable(LiveTable):
    COLUMNS = NODE_COLUMNS

    def show(self, rows: list[dict]) -> None:
        ordered = sorted(rows, key=lambda n: (node_state(n) != "online", n["hostname"]))
        self.replace(
            [
                (
                    n["id"],
                    (
                        n["id"],
                        n["hostname"],
                        Text.assemble(("● online", ONLINE))
                        if node_state(n) == "online"
                        else Text.assemble(("● offline", OFFLINE)),
                        f"{n['os']}/{n['arch']}",
                        str(n.get("cpus", 0)),
                        f"{n.get('mem_bytes', 0) / 1e9:.1f}GB",
                        human_ago(nodes_cmd.seconds_ago(n["last_seen"])),
                    ),
                )
                for n in ordered
            ]
        )


class JobsTable(LiveTable):
    COLUMNS = JOB_COLUMNS

    # Same action the DataTable already has on enter, named so the footer
    # says what it does here.
    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("enter", "select_cursor", "Output", key_display="enter"),
    ]

    def show(self, rows: list[dict], hostnames: dict[str, str]) -> None:
        """hostnames maps node IDs to hostnames so the NODE column reads well."""
        out = []
        for job in rows:
            state = job["status"]
            node_id = job.get("node_id", "") or ""
            out.append(
                (
                    job["id"],
                    (
                        job["id"],
                        Text.assemble((state, JOB_STYLES.get(state, ""))),
                        hostnames.get(node_id, node_id) or "-",
                        str(job.get("attempts", 0)),
                        jobs_cmd.age(job["created_at"]),
                        jobs_cmd.shorten(status_cmd.describe(job)),
                    ),
                )
            )
        self.replace(out)


def totals(rows: list[dict]) -> str:
    """One line for the top bar: how much of the pool is online right now."""
    online = [n for n in rows if node_state(n) == "online"]
    cores = sum(n.get("cpus", 0) for n in online)
    mem = sum(n.get("mem_bytes", 0) for n in online) / 1e9
    return (
        f"{len(online)} online / {len(rows) - len(online)} offline"
        f"  ·  {cores} cores  ·  {mem:.0f} GB"
    )
