import shlex
import sys
from pathlib import Path

import httpx
from rich.console import Console
from rich.progress import (
    BarColumn,
    DownloadColumn,
    Progress,
    TextColumn,
    TransferSpeedColumn,
)

from relay import binaries

ACCENT = "#f0b35a"
OK = "#7bd88f"


def register(sub) -> None:
    p = sub.add_parser(
        "install", help="download relayd and relay-agent for this machine"
    )
    p.add_argument(
        "--version", help="release tag, for example v0.2.6 (default: latest)"
    )
    p.add_argument(
        "--agent-only", action="store_true", help="only relay-agent, for contributors"
    )
    p.set_defaults(func=run)


def short(path: Path) -> str:
    """A paste-ready path: ~ for the home folder, and when it has a space,
    "$HOME/..." in double quotes so the shell still expands it."""
    try:
        rest = str(path.relative_to(Path.home()))
    except ValueError:
        return shlex.quote(str(path)) if " " in str(path) else str(path)
    return f'"$HOME/{rest}"' if " " in rest else f"~/{rest}"


def run(args) -> None:
    console = Console(soft_wrap=True)
    errors = Console(stderr=True, soft_wrap=True)
    names = ("relay-agent",) if args.agent_only else binaries.NAMES
    paths: dict[str, Path] = {}
    with Progress(
        TextColumn("[bold]{task.fields[name]}"),
        BarColumn(bar_width=30, complete_style=ACCENT, finished_style=OK),
        DownloadColumn(),
        TransferSpeedColumn(),
        TextColumn("{task.fields[status]}"),
        console=console,
    ) as bar:
        for name in names:
            task = bar.add_task("dl", name=name, status="", total=None)

            def tick(done: int, total: int | None, task=task) -> None:
                bar.update(task, completed=done, total=total)

            try:
                path, tag, fetched = binaries.download(
                    name, args.version, progress=tick
                )
            except binaries.MissingBinary as exc:
                bar.stop()
                errors.print(f"[red]relay install:[/] {exc}")
                sys.exit(1)
            except httpx.HTTPStatusError as exc:
                bar.stop()
                code = exc.response.status_code
                if code == 404 and args.version:
                    what = f"no release {args.version}"
                elif code == 404:
                    what = (
                        "the latest release has no build for this machine, "
                        "or the repo is not public"
                    )
                else:
                    what = f"download failed: {code}"
                errors.print(f"[red]relay install:[/] {what} ({name})")
                sys.exit(1)
            paths[name] = path
            if fetched:
                bar.update(task, status=f"[{OK}]{tag}[/]")
            else:
                bar.update(
                    task,
                    completed=1,
                    total=1,
                    status=f"[{OK}]{tag}[/] already installed",
                )

    folder = short(paths[names[0]].parent)
    console.print(f"\ninstalled to [{ACCENT}]{folder}[/]\n")
    if "relay-agent" in paths:
        console.print("contribute this machine:")
        console.print(
            f"  [{ACCENT}]{short(paths['relay-agent'])}[/] "
            "--scheduler <address> --pool-token <token>"
        )
    if "relayd" in paths:
        console.print("run a scheduler:")
        console.print(
            f"  [{ACCENT}]relay scheduler start[/]   "
            "(add --public for an internet address)"
        )
    console.print(
        f"\nor add [{ACCENT}]{folder}[/] to your PATH and use the names directly."
    )
