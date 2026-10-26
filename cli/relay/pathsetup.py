"""Put the relay bin folder on the user's PATH, the way uv and rustup do.

On macOS and Linux a line is appended to the shell's startup file. On
Windows the user PATH in the registry is extended. Both are idempotent.
"""

import os
import sys
from pathlib import Path

MARK = "# added by relay install"


def already_on_path(bin_dir: Path, path_env: str | None = None) -> bool:
    env = path_env if path_env is not None else os.environ.get("PATH", "")
    want = os.path.normcase(os.path.normpath(str(bin_dir)))
    return any(
        os.path.normcase(os.path.normpath(e)) == want
        for e in env.split(os.pathsep)
        if e
    )


def _tilde(p: Path, home: Path) -> str:
    try:
        return "~/" + p.relative_to(home).as_posix()
    except ValueError:
        return str(p)


def _dollar_home(p: Path, home: Path) -> str:
    try:
        return "$HOME/" + p.relative_to(home).as_posix()
    except ValueError:
        return str(p)


def rc_file(shell: str, home: Path, platform: str, bin_dir: Path) -> tuple[Path, str]:
    """The startup file for this shell and the exact line to put in it."""
    name = os.path.basename(shell or "")
    target = _dollar_home(bin_dir, home)
    if name == "fish":
        return (
            home / ".config" / "fish" / "config.fish",
            f'fish_add_path "{target}"  {MARK}',
        )
    line = f'export PATH="{target}:$PATH"  {MARK}'
    if name == "bash":
        # macOS Terminal opens login shells, which read .bash_profile.
        return home / (".bash_profile" if platform == "darwin" else ".bashrc"), line
    if name == "zsh" or not name:
        return home / ".zshrc", line
    return home / ".profile", line


def add_to_shell_rc(
    bin_dir: Path, home: Path, shell: str, platform: str
) -> tuple[str, str, bool]:
    """Append the PATH line once. Returns (file shown with ~, how to activate
    now, whether the line was written this time)."""
    rc, line = rc_file(shell, home, platform, bin_dir)
    existing = rc.read_text() if rc.exists() else ""
    wrote = MARK not in existing
    if wrote:
        rc.parent.mkdir(parents=True, exist_ok=True)
        sep = "" if not existing or existing.endswith("\n") else "\n"
        with open(rc, "a") as f:
            f.write(sep + line + "\n")
    shown = _tilde(rc, home)
    if os.path.basename(shell) == "fish":
        how = "open a new terminal"
    else:
        how = f"source {shown}"
    return shown, how, wrote


def add_to_windows_path(bin_dir: Path) -> None:
    """Prepend bin_dir to the user's PATH in the registry and tell Explorer."""
    import ctypes
    import winreg

    with winreg.OpenKey(
        winreg.HKEY_CURRENT_USER, "Environment", 0, winreg.KEY_ALL_ACCESS
    ) as key:
        try:
            current, kind = winreg.QueryValueEx(key, "Path")
        except FileNotFoundError:
            current, kind = "", winreg.REG_EXPAND_SZ
        want = os.path.normcase(os.path.normpath(str(bin_dir)))
        present = any(
            os.path.normcase(os.path.normpath(e)) == want
            for e in current.split(";")
            if e
        )
        if present:
            return
        new = f"{bin_dir};{current}" if current else str(bin_dir)
        winreg.SetValueEx(key, "Path", 0, kind, new)
    # HWND_BROADCAST, WM_SETTINGCHANGE, SMTO_ABORTIFHUNG: new terminals see it.
    ctypes.windll.user32.SendMessageTimeoutW(
        0xFFFF, 0x1A, 0, "Environment", 0x0002, 5000, None
    )


def ensure_on_path(bin_dir: Path) -> str:
    """Make bin_dir reachable by name. Returns a note for the user, or ""
    when it already was."""
    if already_on_path(bin_dir):
        return ""
    if sys.platform == "win32":
        add_to_windows_path(bin_dir)
        return "added to your PATH; open a new terminal and the names just work"
    rc, how, wrote = add_to_shell_rc(
        bin_dir, Path.home(), os.environ.get("SHELL", ""), sys.platform
    )
    verb = "added to" if wrote else "already in"
    return f"{verb} {rc}; restart your shell or run: {how}"
