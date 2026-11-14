"""Enter on a job: what it is, where it ran, and what it printed."""

from typing import ClassVar

import httpx
from textual import work
from textual.app import ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Vertical, VerticalScroll
from textual.screen import ModalScreen
from textual.widgets import Static

from relay.commands import fetch as fetch_cmd
from relay.commands import logs as logs_cmd
from relay.commands import status as status_cmd
from relay.dash.tables import REFRESH_S
from relay.dash.theme import JOB_STYLES

FINISHED = ("done", "failed")


class JobModal(ModalScreen[None]):
    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("r", "refresh", "Refresh"),
        Binding("f", "fetch", "Fetch files"),
        Binding("escape", "dismiss", "Close", priority=True),
    ]

    def __init__(self, job_id: str, hostnames: dict[str, str]) -> None:
        super().__init__()
        self.job_id = job_id
        self.hostnames = hostnames
        self.timer = None
        self.output_text = ""
        self.job: dict | None = None

    def copyable(self) -> str:
        return self.output_text

    def compose(self) -> ComposeResult:
        with Vertical(classes="modal", id="job-modal"):
            yield Static(f"JOB {self.job_id}", classes="title")
            yield Static("loading…", id="job-summary", classes="result")
            with VerticalScroll(id="job-output"):
                yield Static("", id="job-stdout", markup=False)
                yield Static("", id="job-stderr", classes="stderr", markup=False)
            yield Static("r refresh   esc close", classes="keys")

    def on_mount(self) -> None:
        self.load()
        # A job that is still going updates on its own, like the tables.
        self.timer = self.set_interval(REFRESH_S, self.load)

    @work(exclusive=True, thread=True)
    def load(self) -> None:
        app = self.app
        token = app.prof.get("pool_token", "")
        try:
            job = status_cmd.fetch(app.scheduler, self.job_id, token)
            code, out = logs_cmd.fetch_output(app.scheduler, self.job_id, token)
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            app.call_from_thread(self.show_error, f"can't load job: {exc}")
            return
        app.call_from_thread(self.show, job, code, out)

    def show_error(self, msg: str) -> None:
        self.query_one("#job-summary", Static).update(f"[$error]{msg}[/]")

    def show(self, job: dict, code: int, out: dict | None) -> None:
        self.job = job
        state = job["status"]
        node = job.get("node_id") or ""
        ran = status_cmd.runtime(job)
        lines = [
            f"[b]{status_cmd.describe(job)}[/b]",
            "",
            f"status     [{JOB_STYLES.get(state, '')}]{state}[/]",
            f"node       {self.hostnames.get(node, node) or '-'}",
            f"attempts   {job.get('attempts', 0)}",
            f"submitter  {job.get('submitter') or '-'}",
            # Keep the limit visible even after the job finishes.
            f"time limit {status_cmd.time_limit(job)}",
        ]
        if ran:
            lines.append(f"{'running' if state == 'running' else 'ran':<11}{ran}")
        # This modal already refreshes every two seconds, so the value below
        # counts down automatically until the job is done or failed.
        if state == "running":
            lines.append(f"time left  {status_cmd.time_left(job) or '-'}")
        if out is not None:
            lines.append(f"exit code  {out.get('exit_code', 0)}")
        # Files the job sent back, for relay fetch.
        files = job.get("output_files") or []
        if files:
            lines.append("")
            lines.append(f"output files ({len(files)})")
            lines.extend(f"  {f['path']}  {f['bytes']} bytes" for f in files)
            lines.append(f"  f fetch to {fetch_cmd.default_folder(self.job_id)}")
        self.query_one("#job-summary", Static).update("\n".join(lines))

        stdout = self.query_one("#job-stdout", Static)
        stderr = self.query_one("#job-stderr", Static)
        if out is None:
            stdout.update(
                "(no output yet, the job hasn't finished)" if code == 409 else ""
            )
            stderr.update("")
        else:
            stdout.update(out.get("stdout", "") or "(no output)")
            stderr.update(out.get("stderr", ""))
            self.output_text = out.get("stdout", "") + out.get("stderr", "")
        if state in FINISHED and self.timer is not None:
            self.timer.stop()
            self.timer = None

    def action_refresh(self) -> None:
        self.load()

    def action_fetch(self) -> None:
        if not (self.job and self.job.get("output_files")):
            self.app.notify("this job has no output files", severity="warning")
            return
        self.fetch()

    @work(exclusive=True, thread=True, group="fetch")
    def fetch(self) -> None:
        app = self.app
        token = app.prof.get("pool_token", "")
        try:
            folder, files = fetch_cmd.fetch_files(
                app.scheduler, self.job_id, token, job=self.job
            )
        except fetch_cmd.FetchFailed as exc:
            app.call_from_thread(app.notify, str(exc), severity="error")
            return
        except (httpx.HTTPError, httpx.InvalidURL) as exc:
            app.call_from_thread(app.notify, f"can't fetch: {exc}", severity="error")
            return
        if not files:
            app.call_from_thread(app.notify, "this job has no output files")
            return
        n = len(files)
        app.call_from_thread(
            app.notify, f"fetched {n} file{'s' if n != 1 else ''} to {folder.resolve()}"
        )
