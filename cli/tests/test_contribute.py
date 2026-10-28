import argparse
import asyncio
import json
import os
import signal
import subprocess
import sys
from pathlib import Path

import pytest
from textual.widgets import Static

from relay import contribution, profile
from relay.commands import contribute
from relay.commands import jobs as jobs_cmd
from relay.commands import nodes as nodes_cmd
from relay.dash import DashApp

SCHED = "http://test:8080"
SAVED = {
    "scheduler": SCHED,
    "pool_id": "p1",
    "pool_name": "lab",
    "pool_token": "tok",
    "name": "akrm",
}


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    # Keep test PID, log, and profile files away from the real user's config.
    folder = tmp_path / "relay"
    monkeypatch.setattr(profile, "config_dir", lambda: folder)
    return folder


def tracked_agent(cfg, pid=4321):
    cfg.mkdir(parents=True, exist_ok=True)
    (cfg / "relay-agent.pid").write_text(f"{pid}\n", encoding="utf-8")
    (cfg / "relay-agent.state.json").write_text(
        json.dumps({"pid": pid, "pool_id": "p1", "pool_name": "lab"}),
        encoding="utf-8",
    )


def test_start_launches_agent_with_saved_pool(cfg, monkeypatch):
    seen = {}

    class FakeProcess:
        pid = 4321

        def poll(self):
            return None  # The agent is still running.

    def fake_popen(argv, **options):
        seen["argv"] = argv
        seen["options"] = options
        return FakeProcess()

    # The CLI uses the same binary resolver as `relay install`.
    def fake_find(name):
        seen["binary_name"] = name
        return "relay-agent-test"

    monkeypatch.setattr(contribution.binaries, "find", fake_find)
    monkeypatch.setattr(contribution.subprocess, "Popen", fake_popen)
    monkeypatch.setattr(contribution, "STARTUP_SECONDS", 0)

    pid, log_path = contribution.start(SAVED, SCHED)

    assert pid == 4321
    assert seen["binary_name"] == "relay-agent"
    assert seen["argv"] == [
        "relay-agent-test",
        "--scheduler",
        SCHED,
        "--pool-token",
        "tok",
        "--owner",
        "akrm",
    ]
    assert seen["options"]["stdin"] == subprocess.DEVNULL
    assert seen["options"]["stderr"] == subprocess.STDOUT
    assert Path(seen["options"]["stdout"].name) == log_path
    assert log_path == cfg / "relay-agent.log"
    assert (cfg / "relay-agent.pid").read_text(encoding="utf-8") == "4321\n"
    assert json.loads((cfg / "relay-agent.state.json").read_text())["pool_id"] == "p1"

    if sys.platform == "win32":
        assert seen["options"]["creationflags"] == subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        assert seen["options"]["start_new_session"] is True


def test_start_reports_last_log_line_when_agent_exits(cfg, monkeypatch):
    class ExitedProcess:
        pid = 4321

        def poll(self):
            return 1

    def fake_popen(argv, **options):
        # All agent output is redirected into its log file.
        options["stdout"].write("starting agent\ninvalid pool token\n")
        options["stdout"].flush()
        return ExitedProcess()

    monkeypatch.setattr(contribution.binaries, "find", lambda name: "relay-agent-test")
    monkeypatch.setattr(contribution.subprocess, "Popen", fake_popen)

    with pytest.raises(contribution.ContributionError, match="invalid pool token"):
        contribution.start(SAVED, SCHED)

    # Do not leave a PID pointing at a process that already exited.
    assert not (cfg / "relay-agent.pid").exists()
    assert not (cfg / "relay-agent.state.json").exists()


def test_start_rejects_an_existing_agent(cfg, monkeypatch):
    tracked_agent(cfg)
    monkeypatch.setattr(contribution.binaries, "find", lambda name: "relay-agent-test")
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: True)

    with pytest.raises(contribution.ContributionError, match="already contributing"):
        contribution.start(SAVED, SCHED)


def test_status_reads_the_tracked_pool(cfg, monkeypatch):
    tracked_agent(cfg)
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: True)

    assert contribution.status() == {
        "pid": 4321,
        "pool_id": "p1",
        "pool_name": "lab",
    }


def test_status_removes_stale_pid_and_state(cfg, monkeypatch):
    tracked_agent(cfg)
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: False)

    assert contribution.status() is None
    assert not (cfg / "relay-agent.pid").exists()
    assert not (cfg / "relay-agent.state.json").exists()


def test_stop_signals_the_tracked_process(cfg, monkeypatch):
    tracked_agent(cfg)
    signals = []

    def fake_kill(pid, sig):
        signals.append((pid, sig))

    # Before the signal the process is alive; afterward it has exited.
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: not signals)
    monkeypatch.setattr(contribution.os, "kill", fake_kill)

    stopped = contribution.stop()

    expected_signal = (
        signal.CTRL_BREAK_EVENT if sys.platform == "win32" else signal.SIGTERM
    )
    assert signals == [(4321, expected_signal)]
    assert stopped["pool_name"] == "lab"
    assert not (cfg / "relay-agent.pid").exists()
    assert not (cfg / "relay-agent.state.json").exists()


def test_cli_without_login_exits_with_hint(cfg, capsys):
    with pytest.raises(SystemExit) as exc:
        contribute.run_start(argparse.Namespace(scheduler=SCHED))

    assert exc.value.code == 1
    assert "relay login" in capsys.readouterr().err


def test_cli_contribute_uses_saved_login(cfg, monkeypatch, capsys):
    profile.save(SAVED)
    seen = []

    def fake_start(prof, scheduler):
        seen.append((prof, scheduler))
        return 4321, cfg / "relay-agent.log"

    monkeypatch.setattr(contribution, "start", fake_start)

    contribute.run_start(argparse.Namespace(scheduler=SCHED))

    assert seen == [(SAVED, SCHED)]
    assert "contributing to lab (PID 4321)" in capsys.readouterr().out


def test_cli_contribute_status_and_stop(cfg, monkeypatch, capsys):
    state = {"pid": 4321, "pool_name": "lab", "pool_id": "p1"}
    monkeypatch.setattr(contribution, "status", lambda: state)
    monkeypatch.setattr(contribution, "stop", lambda: state)

    contribute.run_status(argparse.Namespace())
    assert "contributing to lab (p1)" in capsys.readouterr().out

    contribute.run_stop(argparse.Namespace())
    assert "stopped contributing to lab (p1)" in capsys.readouterr().out


def test_dashboard_a_starts_and_stops_agent(cfg, monkeypatch):
    profile.save(SAVED)
    monkeypatch.setattr(nodes_cmd, "fetch", lambda scheduler, token: [])
    monkeypatch.setattr(jobs_cmd, "fetch", lambda scheduler, token: [])

    state = {"running": False}
    calls = []

    def fake_status():
        return {"pid": 4321, "pool_name": "lab"} if state["running"] else None

    def fake_start(prof, scheduler):
        calls.append(("start", prof["pool_token"], scheduler))
        state["running"] = True
        return 4321, cfg / "relay-agent.log"

    def fake_stop():
        calls.append(("stop",))
        state["running"] = False
        return {"pid": 4321, "pool_name": "lab"}

    monkeypatch.setattr(contribution, "status", fake_status)
    monkeypatch.setattr(contribution, "start", fake_start)
    monkeypatch.setattr(contribution, "stop", fake_stop)

    app = DashApp(SCHED)

    async def run():
        async with app.run_test(size=(150, 40)) as pilot:
            await app.workers.wait_for_complete()
            await pilot.pause()
            indicator = app.query_one("#contribution", Static)
            assert "not contributing" in str(indicator.render())

            # Pressing A uses contribution.start() from the CLI layer.
            await pilot.press("a")
            await app.workers.wait_for_complete()
            await pilot.pause()
            assert "this machine: contributing" in str(indicator.render())
            assert indicator.has_class("running")

            # Pressing A a second time uses contribution.stop().
            await pilot.press("a")
            await app.workers.wait_for_complete()
            await pilot.pause()
            assert "not contributing" in str(indicator.render())
            assert not indicator.has_class("running")

    asyncio.run(run())
    assert calls == [("start", "tok", SCHED), ("stop",)]


def test_dashboard_ignores_toggle_while_busy(cfg, monkeypatch):
    profile.save(SAVED)
    monkeypatch.setattr(nodes_cmd, "fetch", lambda scheduler, token: [])
    monkeypatch.setattr(jobs_cmd, "fetch", lambda scheduler, token: [])
    calls = []
    monkeypatch.setattr(contribution, "status", lambda: None)
    monkeypatch.setattr(contribution, "start", lambda *args: calls.append("start"))

    app = DashApp(SCHED)

    async def run():
        async with app.run_test(size=(150, 40)):
            await app.workers.wait_for_complete()
            app._contribution_busy = True
            app.action_toggle_contribution()

    asyncio.run(run())
    assert calls == []


@pytest.mark.skipif(sys.platform == "win32", reason="zombies are a POSIX thing")
def test_pid_alive_reaps_an_exited_child():
    """A child that exited but was never waited on is a zombie. os.kill(pid, 0)
    still succeeds on it, so _pid_alive has to collect it and report dead."""
    import time

    proc = subprocess.Popen(["true"], start_new_session=True)
    # Wait for it to exit without reaping it: ps shows state Z for a zombie.
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        out = subprocess.run(
            ["ps", "-o", "stat=", "-p", str(proc.pid)],
            capture_output=True,
            text=True,
            check=False,
        ).stdout.strip()
        if out.startswith("Z") or not out:
            break
        time.sleep(0.05)
    os.kill(proc.pid, 0)  # still visible to the kernel
    assert contribution._pid_alive(proc.pid) is False
    assert contribution._pid_alive(proc.pid) is False  # and stays dead
