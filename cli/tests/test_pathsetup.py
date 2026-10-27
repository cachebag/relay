import os
from pathlib import Path

import pytest

from relay import pathsetup

BIN = "Library/Application Support/relay/bin"


@pytest.fixture
def home(tmp_path):
    (tmp_path / BIN).mkdir(parents=True)
    return tmp_path


def test_already_on_path_normalises(home):
    bin_dir = home / BIN
    assert pathsetup.already_on_path(bin_dir, f"/usr/bin{os.pathsep}{bin_dir}/")
    assert not pathsetup.already_on_path(bin_dir, "/usr/bin")


@pytest.mark.parametrize(
    "shell, platform, rc, start",
    [
        ("/bin/zsh", "darwin", ".zshrc", 'export PATH="$HOME/'),
        ("/bin/bash", "darwin", ".bash_profile", 'export PATH="$HOME/'),
        ("/usr/bin/bash", "linux", ".bashrc", 'export PATH="$HOME/'),
        (
            "/opt/homebrew/bin/fish",
            "darwin",
            ".config/fish/config.fish",
            "fish_add_path",
        ),
        ("", "linux", ".zshrc", 'export PATH="$HOME/'),
        ("/bin/ksh", "linux", ".profile", 'export PATH="$HOME/'),
    ],
)
def test_rc_file_per_shell(home, shell, platform, rc, start):
    path, line = pathsetup.rc_file(shell, home, platform, home / BIN)
    assert path == home / rc
    assert line.startswith(start) and BIN in line and pathsetup.MARK in line


def test_add_to_shell_rc_appends_once_and_keeps_existing(home):
    rc = home / ".zshrc"
    rc.write_text("alias ll='ls -l'")  # no trailing newline
    shown, how, wrote = pathsetup.add_to_shell_rc(
        home / BIN, home, "/bin/zsh", "darwin"
    )
    assert shown == "~/.zshrc" and how == "source ~/.zshrc" and wrote
    text = rc.read_text()
    assert text.startswith("alias ll='ls -l'\n")
    assert text.count(pathsetup.MARK) == 1
    assert not pathsetup.add_to_shell_rc(home / BIN, home, "/bin/zsh", "darwin")[2]
    assert rc.read_text().count(pathsetup.MARK) == 1


def test_add_to_shell_rc_creates_fish_config(home):
    shown, how, _ = pathsetup.add_to_shell_rc(
        home / BIN, home, "/usr/bin/fish", "linux"
    )
    assert shown == "~/.config/fish/config.fish" and how == "open a new terminal"
    assert 'fish_add_path "$HOME/' in (home / ".config/fish/config.fish").read_text()


def test_ensure_on_path_skips_when_present(home, monkeypatch):
    monkeypatch.setenv("PATH", str(home / BIN))
    assert pathsetup.ensure_on_path(home / BIN) == ""


def test_ensure_on_path_posix(home, monkeypatch):
    monkeypatch.setattr(pathsetup.sys, "platform", "darwin")
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: home))
    monkeypatch.setenv("PATH", "/usr/bin")
    monkeypatch.setenv("SHELL", "/bin/zsh")
    note = pathsetup.ensure_on_path(home / BIN)
    assert note == "added to ~/.zshrc; restart your shell or run: source ~/.zshrc"


def test_windows_path_is_prepended_once(monkeypatch):
    """Drive the registry branch with fake winreg and ctypes modules."""
    import sys
    import types

    store = {"Path": ("C:\\Windows", 2)}
    calls = []

    class Key:
        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

    fake_winreg = types.SimpleNamespace(
        HKEY_CURRENT_USER=0,
        KEY_ALL_ACCESS=0,
        REG_EXPAND_SZ=2,
        OpenKey=lambda *a: Key(),
        QueryValueEx=lambda key, name: store[name],
        SetValueEx=lambda key, name, r, kind, value: store.__setitem__(
            name, (value, kind)
        ),
    )
    fake_ctypes = types.SimpleNamespace(
        windll=types.SimpleNamespace(
            user32=types.SimpleNamespace(SendMessageTimeoutW=lambda *a: calls.append(a))
        )
    )
    monkeypatch.setitem(sys.modules, "winreg", fake_winreg)
    monkeypatch.setitem(sys.modules, "ctypes", fake_ctypes)
    bin_dir = Path("C:\\Users\\me\\AppData\\Roaming\\relay\\bin")
    pathsetup.add_to_windows_path(bin_dir)
    assert store["Path"][0] == f"{bin_dir};C:\\Windows"
    assert len(calls) == 1
    pathsetup.add_to_windows_path(bin_dir)
    assert store["Path"][0] == f"{bin_dir};C:\\Windows"
