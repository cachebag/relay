import subprocess

import pytest

from relay.dash import picker

DESKTOP = {"DISPLAY": ":0"}


def no_tmux():
    raise AssertionError("tmux was asked outside a tmux session")


@pytest.mark.parametrize(
    ("env", "platform", "expected"),
    [
        ({}, "darwin", None),
        (DESKTOP, "linux", None),
        ({"WAYLAND_DISPLAY": "wayland-0"}, "linux", None),
        ({"SESSIONNAME": "Console"}, "win32", None),
        ({"SSH_CONNECTION": "10.0.0.2 1 10.0.0.3 22"}, "darwin", "SSH"),
        ({"SSH_TTY": "/dev/pts/3", **DESKTOP}, "linux", "SSH"),
        ({"SSH_CLIENT": "10.0.0.2 1 22"}, "win32", "SSH"),
        ({"SESSIONNAME": "RDP-Tcp#4"}, "win32", "Remote Desktop"),
        ({}, "linux", "no desktop session"),
    ],
)
def test_remote_says_why_a_dialog_would_not_reach_the_user(env, platform, expected):
    reason = picker.remote(env, platform, tmux=no_tmux)
    if expected is None:
        assert reason is None
    else:
        assert expected in reason


def test_inside_tmux_the_attached_client_decides():
    # Started over SSH, now attached from the machine's own terminal.
    stale = {"TMUX": "/tmp/tmux-501/default,1,0", "SSH_CONNECTION": "a 1 b 22"}
    assert picker.remote(stale, "darwin", tmux=lambda: False) is None
    # Started at the machine, now attached over SSH.
    local = {"TMUX": "/tmp/tmux-501/default,1,0"}
    assert "SSH" in picker.remote(local, "darwin", tmux=lambda: True)
    # tmux can't say, so the process environment stands.
    assert "SSH" in picker.remote(stale, "darwin", tmux=lambda: None)
    assert picker.remote(local, "darwin", tmux=lambda: None) is None


def test_command_for_each_desktop():
    assert picker.command("folder", {}, "darwin")[:2] == ["osascript", "-e"]
    assert "choose folder" in picker.command("folder", {}, "darwin")[2]
    assert "choose file" in picker.command("file", {}, "darwin")[2]

    win = picker.command("folder", {}, "win32")
    assert win[0] == "powershell"
    # File Explorer's own dialog first, the old tree view only as a fallback.
    assert win[-1].index("RelayFolderPicker]::Pick") < win[-1].index(
        "FolderBrowserDialog"
    )
    assert "TITLE" not in win[-1] and "'Folder to send as a job'" in win[-1]
    file = picker.command("file", {}, "win32")[-1]
    assert "OpenFileDialog" in file and "'Script to send as a job'" in file

    both = lambda name: f"/usr/bin/{name}"
    assert picker.command("folder", {}, "linux", both)[:2] == [
        "zenity",
        "--file-selection",
    ]
    assert "--directory" in picker.command("folder", {}, "linux", both)
    assert "--directory" not in picker.command("file", {}, "linux", both)
    kde = {"XDG_CURRENT_DESKTOP": "KDE"}
    assert picker.command("file", kde, "linux", both)[0] == "kdialog"
    only_kdialog = lambda name: name == "kdialog" and "/usr/bin/kdialog"
    assert picker.command("folder", {}, "linux", only_kdialog)[0] == "kdialog"
    with pytest.raises(picker.PickerUnavailable, match="zenity or kdialog"):
        picker.command("folder", {}, "linux", lambda name: None)


class FakeDialog:
    """Stands in for the dialog program. waits is how many 0.2 s polls pass
    before it answers."""

    def __init__(self, out="", err="", code=0, waits=0):
        self.out, self.err, self.returncode, self.waits = out, err, code, waits
        self.killed = False

    def communicate(self, timeout=None):
        if self.waits and not self.killed:
            self.waits -= 1
            raise subprocess.TimeoutExpired("dialog", timeout)
        return self.out, self.err

    def kill(self):
        self.killed = True


def fake_popen(monkeypatch, dialog):
    seen = []

    def popen(argv, **kwargs):
        seen.append(argv)
        if isinstance(dialog, Exception):
            raise dialog
        return dialog

    monkeypatch.setattr(picker.subprocess, "Popen", popen)
    return seen


def test_pick_returns_the_path_without_a_trailing_slash(monkeypatch):
    seen = fake_popen(monkeypatch, FakeDialog("/Users/me/crunch/\n", waits=2))
    assert picker.pick("folder", env={}, platform="darwin") == "/Users/me/crunch"
    assert seen[0][0] == "osascript"


def test_pick_is_none_when_the_dialog_is_cancelled(monkeypatch):
    cancelled = FakeDialog(err="execution error: User canceled. (-128)\n", code=1)
    fake_popen(monkeypatch, cancelled)
    assert picker.pick("folder", env={}, platform="darwin") is None
    fake_popen(monkeypatch, FakeDialog(code=0))
    assert picker.pick("file", env={}, platform="win32") is None


def test_pick_closes_the_dialog_when_asked_to_stop(monkeypatch):
    dialog = FakeDialog("/late", waits=100)
    fake_popen(monkeypatch, dialog)
    assert picker.pick("folder", stop=lambda: True, env={}, platform="darwin") is None
    assert dialog.killed


def test_pick_refuses_over_ssh_without_opening_anything(monkeypatch):
    seen = fake_popen(monkeypatch, FakeDialog("/x"))
    with pytest.raises(picker.PickerUnavailable, match="SSH session; type the path"):
        picker.pick("folder", env={"SSH_CONNECTION": "a 1 b 22"}, platform="darwin")
    assert seen == []


def test_pick_says_when_the_dialog_program_fails(monkeypatch):
    fake_popen(monkeypatch, FileNotFoundError("no osascript"))
    with pytest.raises(picker.PickerUnavailable, match="can't open the file picker"):
        picker.pick("folder", env={}, platform="darwin")
    fake_popen(monkeypatch, FakeDialog(err="cannot open display\n", code=2))
    with pytest.raises(picker.PickerUnavailable, match="cannot open display"):
        picker.pick("folder", env=DESKTOP, platform="darwin")


def test_show_opens_the_folder_in_the_desktops_file_manager(monkeypatch, tmp_path):
    seen = fake_popen(monkeypatch, FakeDialog())
    picker.show(tmp_path, env={}, platform="darwin")
    picker.show(tmp_path, env=DESKTOP, platform="linux")
    assert seen == [["open", str(tmp_path)], ["xdg-open", str(tmp_path)]]

    started = []
    monkeypatch.setattr(picker.os, "startfile", started.append, raising=False)
    picker.show(tmp_path, env={}, platform="win32")
    assert started == [tmp_path] and len(seen) == 2


def test_show_refuses_over_ssh_and_says_where_the_folder_is(monkeypatch, tmp_path):
    seen = fake_popen(monkeypatch, FakeDialog())
    with pytest.raises(picker.PickerUnavailable, match="SSH session") as exc:
        picker.show(tmp_path, env={"SSH_TTY": "/dev/pts/3"}, platform="darwin")
    assert str(tmp_path) in str(exc.value) and seen == []


def test_show_says_when_there_is_nothing_to_open_it_with(monkeypatch, tmp_path):
    fake_popen(monkeypatch, FileNotFoundError("no xdg-open"))
    with pytest.raises(picker.PickerUnavailable, match="can't open the folder"):
        picker.show(tmp_path, env=DESKTOP, platform="linux")
