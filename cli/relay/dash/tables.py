"""The nodes and jobs tables. Each one takes the scheduler's rows and draws them."""

from rich.text import Text
from textual.widgets import DataTable

from relay.commands import jobs as jobs_cmd
from relay.commands import nodes as nodes_cmd
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


class NodesTable(DataTable):
    def on_mount(self) -> None:
        # Nothing to do on a node yet, so no cursor to wander around.
        self.cursor_type = "none"
        self.add_columns(*NODE_COLUMNS)

    def show(self, rows: list[dict]) -> None:
        self.clear()
        for n in sorted(rows, key=lambda n: (node_state(n) != "online", n["hostname"])):
            online = node_state(n) == "online"
            self.add_row(
                n["id"],
                n["hostname"],
                Text("● online", style=ONLINE)
                if online
                else Text("● offline", style=OFFLINE),
                f"{n['os']}/{n['arch']}",
                str(n.get("cpus", 0)),
                f"{n.get('mem_bytes', 0) / 1e9:.1f}GB",
                human_ago(nodes_cmd.seconds_ago(n["last_seen"])),
                key=n["id"],
            )


class JobsTable(DataTable):
    def on_mount(self) -> None:
        self.cursor_type = "row"
        self.add_columns(*JOB_COLUMNS)

    def show(self, rows: list[dict], hostnames: dict[str, str]) -> None:
        """hostnames maps node IDs to hostnames so the NODE column reads well."""
        self.clear()
        for job in rows:
            state = job["status"]
            node_id = job.get("node_id", "") or ""
            self.add_row(
                job["id"],
                Text(state, style=JOB_STYLES.get(state, "")),
                hostnames.get(node_id, node_id) or "-",
                str(job.get("attempts", 0)),
                jobs_cmd.age(job["created_at"]),
                jobs_cmd.shorten(" ".join(job["command"])),
                key=job["id"],
            )


def totals(rows: list[dict]) -> str:
    """One line for the top bar: how much of the pool is online right now."""
    online = [n for n in rows if node_state(n) == "online"]
    cores = sum(n.get("cpus", 0) for n in online)
    mem = sum(n.get("mem_bytes", 0) for n in online) / 1e9
    return (
        f"{len(online)} online / {len(rows) - len(online)} offline"
        f"  ·  {cores} cores  ·  {mem:.0f} GB"
    )
