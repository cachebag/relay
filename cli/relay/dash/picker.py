"""ctrl+o and ctrl+f on the new job screen: the desktop's own file dialog,
for whoever is sitting at this machine."""

import os
import shutil
import subprocess
import sys
from collections.abc import Callable, Mapping

TITLES = {"folder": "Folder to send as a job", "file": "Script to send as a job"}
SSH_VARS = ("SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY")

# The dialog opens above other windows; a plain one can end up behind the
# terminal. Nothing is printed on cancel.
WINDOWS = """
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Windows.Forms
$owner = New-Object System.Windows.Forms.Form -Property @{{TopMost = $true}}
$dialog = New-Object System.Windows.Forms.{dialog}
$dialog.{title} = '{text}'
if ($dialog.ShowDialog($owner) -eq 'OK') {{ [Console]::Out.Write($dialog.{result}) }}
"""


class PickerUnavailable(Exception):
    """No dialog can reach the person at the keyboard; str() says why."""


def tmux_ssh() -> bool | None:
    """Whether the client attached to this tmux session came in over SSH, or
    None when tmux can't say. The process environment only knows how the
    session was first started, and tmux updates its own copy on every attach."""
    try:
        out = subprocess.run(
            ["tmux", "show-environment", "SSH_CONNECTION"],
            capture_output=True,
            check=False,
            text=True,
            timeout=2,
        ).stdout.strip()
    except OSError, subprocess.SubprocessError:
        return None
    if out.startswith("SSH_CONNECTION="):
        return True
    if out == "-SSH_CONNECTION":
        return False
    return None


def remote(
    env: Mapping[str, str],
    platform: str,
    tmux: Callable[[], bool | None] = tmux_ssh,
) -> str | None:
    """Why a dialog opened here would not be in front of the person typing,
    or None when they are at this machine's own desktop."""
    ssh = any(env.get(name) for name in SSH_VARS)
    if env.get("TMUX"):
        attached = tmux()
        if attached is not None:
            ssh = attached
    if ssh:
        return "this is an SSH session"
    if platform == "win32":
        rdp = env.get("SESSIONNAME", "").upper().startswith("RDP-")
        return "this is a Remote Desktop session" if rdp else None
    if platform == "darwin" or env.get("DISPLAY") or env.get("WAYLAND_DISPLAY"):
        return None
    return "there is no desktop session here"


def command(
    kind: str,
    env: Mapping[str, str],
    platform: str,
    which: Callable[[str], str | None] = shutil.which,
) -> list[str]:
    """The program that shows this desktop's dialog for a folder or a file
    and prints the pick."""
    title = TITLES[kind]
    if platform == "darwin":
        return [
            "osascript",
            "-e",
            f'POSIX path of (choose {kind} with prompt "{title}")',
        ]
    if platform == "win32":
        dialog, title_field, result = (
            ("FolderBrowserDialog", "Description", "SelectedPath")
            if kind == "folder"
            else ("OpenFileDialog", "Title", "FileName")
        )
        script = WINDOWS.format(
            dialog=dialog, title=title_field, text=title, result=result
        )
        return ["powershell", "-NoProfile", "-STA", "-Command", script]
    home = os.path.expanduser("~")
    tools = {
        "zenity": ["zenity", "--file-selection", f"--title={title}"]
        + (["--directory"] if kind == "folder" else []),
        "kdialog": [
            "kdialog",
            "--title",
            title,
            "--getexistingdirectory" if kind == "folder" else "--getopenfilename",
            home,
        ],
    }
    # Each desktop's own dialog first, then whichever is installed.
    kde = "KDE" in env.get("XDG_CURRENT_DESKTOP", "").upper()
    for name in ("kdialog", "zenity") if kde else ("zenity", "kdialog"):
        if which(name):
            return tools[name]
    raise PickerUnavailable(
        "the file picker needs zenity or kdialog; install one or type the path"
    )


def pick(
    kind: str,
    stop: Callable[[], bool] | None = None,
    env: Mapping[str, str] | None = None,
    platform: str = sys.platform,
) -> str | None:
    """The folder or file picked in the desktop's dialog, or None when the
    dialog was cancelled. Closes the dialog once stop() is true, so an open
    one can't hold up quitting the dash."""
    env = os.environ if env is None else env
    reason = remote(env, platform)
    if reason:
        raise PickerUnavailable(
            f"the file picker opens on this machine's own screen, and {reason}; "
            "type the path"
        )
    argv = command(kind, env, platform)
    try:
        proc = subprocess.Popen(
            argv,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
    except OSError as exc:
        raise PickerUnavailable(
            f"can't open the file picker ({exc}); type the path"
        ) from exc
    while True:
        try:
            out, err = proc.communicate(timeout=0.2)
            break
        except subprocess.TimeoutExpired:
            if stop is not None and stop():
                proc.kill()
                proc.communicate()
                return None
    path = out.strip("\r\n")
    if not path:
        # Cancel is exit 1 from osascript, zenity and kdialog, and 0 from
        # the PowerShell script.
        if proc.returncode in (0, 1):
            return None
        why = err.strip().splitlines()[-1] if err.strip() else f"exit {proc.returncode}"
        raise PickerUnavailable(f"the file picker failed ({why}); type the path")
    return path.rstrip("/") or "/"
