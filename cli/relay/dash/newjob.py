"""j on the dashboard: send a folder or one script as a job, like
relay submit --dir / --file, and see what will go before it goes."""

import argparse
import os
import shlex
from collections.abc import Callable
from pathlib import Path
from typing import ClassVar

import httpx
from rich.markup import escape
from textual import on, work
from textual.app import ComposeResult
from textual.binding import Binding, BindingType
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.suggester import Suggester
from textual.widgets import Input, Label, Select, Static
from textual.worker import get_current_worker

from relay.commands import submit as submit_cmd

TARGETS = ("any", "linux", "darwin", "windows")
# The preview stops counting here. A job's own folder is far smaller, and a
# path typed one character at a time passes through ~ on the way.
SCAN_LIMIT = 20_000
SEPARATORS = ("/", os.sep)


class ScanStopped(Exception):
    """The preview this scan was for is no longer wanted."""


class TooManyFiles(Exception):
    pass


def human_bytes(n: int) -> str:
    for unit in ("B", "KB", "MB"):
        if n < 1024:
            return f"{n:.0f} {unit}" if unit == "B" else f"{n:.1f} {unit}"
        n /= 1024
    return f"{n:.1f} GB"


def scan(
    root: Path,
    stop: Callable[[], bool] | None = None,
    limit: int = SCAN_LIMIT,
) -> tuple[int, int]:
    """Files and bytes that would be sent from a folder, skipping the same
    folders build_bundle does without walking into them. Raises ScanStopped
    once stop() is true and TooManyFiles past limit, so a huge folder can't
    keep a thread busy after nobody wants the answer."""
    files = size = 0
    for here, dirs, names in os.walk(root):
        if stop is not None and stop():
            raise ScanStopped
        dirs[:] = [d for d in dirs if d not in submit_cmd.SKIP_DIRS]
        for name in names:
            path = Path(here, name)
            if path.is_symlink() or not path.is_file():
                continue
            files += 1
            if files > limit:
                raise TooManyFiles
            size += path.stat().st_size
    return files, size


def complete_path(text: str) -> str | None:
    """The typed path extended to the first folder or file that starts with
    its last part, or None when there is nothing to add. Folders get a
    trailing separator. What was typed is kept as typed, ~ included."""
    if text == "~":
        return "~" + os.sep
    cut = max(text.rfind(sep) for sep in SEPARATORS) + 1
    typed_dir, prefix = text[:cut], text[cut:]
    if not prefix:
        return None
    sep = typed_dir[-1] if typed_dir else os.sep
    try:
        with os.scandir(Path(typed_dir or ".").expanduser()) as entries:
            found = sorted(
                (e.name, e.is_dir()) for e in entries if e.name.startswith(prefix)
            )
    except OSError:
        return None
    for name, is_dir in found:
        done = typed_dir + name + (sep if is_dir else "")
        if done != text:
            return done
    return None


class PathSuggester(Suggester):
    def __init__(self) -> None:
        # Folders change under us, so every keystroke looks again.
        super().__init__(use_cache=False, case_sensitive=True)

    async def get_suggestion(self, value: str) -> str | None:
        return complete_path(value)


class PathInput(Input):
    """The folder or script box. Tab takes the completion shown in grey, and
    moves to the next field when there is none."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("tab", "complete", "Complete", show=False),
    ]

    def action_complete(self) -> None:
        done = complete_path(self.value)
        if done is None:
            self.screen.focus_next()
            return
        self.value = done
        self.cursor_position = len(done)


def preview(
    path_text: str, entry: str, stop: Callable[[], bool] | None = None
) -> tuple[bool, str]:
    """(ok, what the job will be) for the path and entry typed so far."""
    if not path_text:
        return False, "pick a folder with a run.sh (or run.ps1, run.cmd), or one script"
    path = Path(path_text).expanduser()
    if path.is_file():
        if entry:
            return False, "an entry is only for folders; a script runs itself"
        return True, f"script  {path.name}, {human_bytes(path.stat().st_size)}"
    if not path.is_dir():
        return False, f"{path_text} is not a folder or a file"
    try:
        files, size = scan(path, stop)
    except TooManyFiles:
        return False, (
            f"{path_text} has more than {SCAN_LIMIT:,} files; "
            "pick the folder with just the job's files"
        )
    lines = [f"folder  {files} files, {human_bytes(size)} before packing"]
    if entry:
        if not (path / entry).is_file():
            return False, f"{entry} is not in {path_text}"
        lines.append(f"runs    {entry}")
    else:
        found = [n for n in submit_cmd.DEFAULT_ENTRIES if (path / n).is_file()]
        if not found:
            return False, (
                f"{path_text} has no run.sh, run.ps1 or run.cmd; "
                "name the file to run under entry"
            )
        lines.append(f"runs    {', '.join(found)}")
    return True, "\n".join(lines)


class NewJobModal(ModalScreen[dict | None]):
    """Dismisses with the submitted job, or None on cancel."""

    BINDINGS: ClassVar[list[BindingType]] = [
        Binding("escape", "cancel", "Cancel", priority=True),
    ]

    def __init__(self, last: dict | None = None) -> None:
        super().__init__()
        # The dashboard remembers the last job's fields so a rerun is one key.
        self.last = last or {}

    def compose(self) -> ComposeResult:
        last = self.last
        with Vertical(classes="modal", id="newjob-modal"):
            yield Static("NEW JOB", classes="title")
            yield Static(
                "Send a folder or one script with the job. Whatever it writes "
                "to out/ comes back with the result.",
                classes="hint",
            )
            yield Static("folder or script", classes="label")
            yield PathInput(
                last.get("path", ""),
                placeholder="./crunch or analyze.py",
                suggester=PathSuggester(),
                id="job-path",
            )
            yield Static("", id="job-preview", classes="result")
            with Horizontal(classes="row"):
                with Vertical():
                    yield Static("entry (folders only)", classes="label")
                    yield Input(
                        last.get("entry", ""),
                        placeholder="run.sh, run.ps1 or run.cmd",
                        id="job-entry",
                    )
                with Vertical():
                    yield Static("arguments", classes="label")
                    yield Input(last.get("args", ""), placeholder="none", id="job-args")
            with Horizontal(classes="row"):
                with Vertical():
                    yield Static("runs on", classes="label")
                    yield Select(
                        [(t, t) for t in TARGETS],
                        value=last.get("target", "any"),
                        allow_blank=False,
                        id="job-target",
                    )
                with Vertical():
                    yield Static("time limit", classes="label")
                    yield Input(
                        last.get("timeout", ""),
                        placeholder="1h (default), up to 24h",
                        id="job-timeout",
                    )
            yield Label("", id="job-status", classes="result", markup=False)
            yield Static(
                "enter send   tab complete, then next field   esc cancel",
                classes="keys",
            )

    def on_mount(self) -> None:
        self.query_one("#job-path", Input).focus()
        self.refresh_preview()

    def fields(self) -> dict:
        return {
            "path": self.query_one("#job-path", Input).value.strip(),
            "entry": self.query_one("#job-entry", Input).value.strip(),
            "args": self.query_one("#job-args", Input).value,
            "target": str(self.query_one("#job-target", Select).value),
            "timeout": self.query_one("#job-timeout", Input).value.strip(),
        }

    def say(self, msg: str, error: bool = False) -> None:
        status = self.query_one("#job-status", Label)
        status.update(msg)
        status.set_class(error, "failed")

    @on(Input.Changed, "#job-path, #job-entry")
    def refresh_preview(self) -> None:
        f = self.fields()
        self.show_preview(f["path"], f["entry"])

    @work(exclusive=True, thread=True, group="preview")
    def show_preview(self, path: str, entry: str) -> None:
        # A big folder takes a moment to count, so it happens off the UI. The
        # count gives up as soon as a newer path or closing the screen cancels
        # this worker; a thread left walking ~ would hold up quitting the dash.
        worker = get_current_worker()
        try:
            ok, text = preview(path, entry, stop=lambda: worker.is_cancelled)
        except ScanStopped:
            return
        except OSError as exc:
            ok, text = False, str(exc)
        if worker.is_cancelled:
            return
        markup = escape(text) if ok else f"[$warning]{escape(text)}[/]"
        self.app.call_from_thread(self.apply_preview, path, entry, markup)

    def apply_preview(self, path: str, entry: str, markup: str) -> None:
        # A slow count for an older path mustn't land over a newer one.
        f = self.fields()
        if (f["path"], f["entry"]) == (path, entry):
            self.query_one("#job-preview", Static).update(markup)

    @on(Input.Submitted)
    def on_submit(self) -> None:
        f = self.fields()
        if not f["path"]:
            # Path("") is ".", which would send whatever folder relay dash runs in.
            self.say("pick a folder or a script first", error=True)
            return
        try:
            args = shlex.split(f["args"])
        except ValueError as exc:
            self.say(f"can't parse the arguments: {exc}", error=True)
            return
        timeout = None
        if f["timeout"]:
            try:
                timeout = submit_cmd.parse_job_timeout(f["timeout"])
            except argparse.ArgumentTypeError as exc:
                self.say(str(exc).replace("--timeout", "time limit"), error=True)
                return
        path = Path(f["path"]).expanduser()
        is_file = path.is_file()
        self.say("packing…")
        # Remember the pick for next time, even if the send fails.
        self.app.last_job = f
        self.send(
            f,
            str(path) if not is_file else None,
            str(path) if is_file else None,
            f["entry"] or None,
            args,
            timeout,
        )

    @work(exclusive=True, thread=True, group="send")
    def send(
        self,
        fields: dict,
        dir_arg: str | None,
        file_arg: str | None,
        entry: str | None,
        args: list[str],
        timeout: int | None,
    ) -> None:
        app = self.app
        try:
            bundle, entry = submit_cmd.make_bundle(dir_arg, file_arg, entry)
        except (submit_cmd.BundleError, OSError) as exc:
            # The CLI's words name its flags; here they're fields.
            msg = str(exc).replace("--entry", "an entry").replace("--dir", "a folder")
            app.call_from_thread(self.say, msg, True)
            return
        app.call_from_thread(self.say, f"sending {human_bytes(len(bundle))}…")
        prof = app.prof
        try:
            job = submit_cmd.submit(
                app.scheduler,
                args,
                prof["pool_token"],
                prof.get("name", ""),
                os=None if fields["target"] == "any" else fields["target"],
                timeout_seconds=timeout,
                bundle=bundle,
                entry=entry,
            )
        except httpx.HTTPStatusError as exc:
            # The scheduler says what was wrong with the files.
            text = exc.response.text.strip() or str(exc.response.status_code)
            app.call_from_thread(self.say, f"submit failed: {text}", True)
            return
        except (httpx.HTTPError, httpx.InvalidURL, ValueError) as exc:
            app.call_from_thread(self.say, f"submit failed: {exc}", True)
            return
        app.call_from_thread(self.dismiss, {"job": job, "fields": fields})

    def action_cancel(self) -> None:
        self.dismiss(None)
