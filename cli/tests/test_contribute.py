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
SECOND = {
    **SAVED,
    "scheduler": "http://other:8080",
    "pool_id": "p2",
    "pool_name": "second",
    "pool_token": "tok2",
}


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    # Keep test PID, log, and profile files away from the real user's config.
    folder = tmp_path / "relay"
    monkeypatch.setattr(profile, "config_dir", lambda: folder)
    return folder


def tracked_agent(cfg, pid=4321, pool_id="p1", pool_name="lab", legacy=False):
    cfg.mkdir(parents=True, exist_ok=True)
    prefix = "relay-agent" if legacy else f"relay-agent.{pool_id}"
    (cfg / f"{prefix}.pid").write_text(f"{pid}\n", encoding="utf-8")
    (cfg / f"{prefix}.state.json").write_text(
        json.dumps({"pid": pid, "pool_id": pool_id, "pool_name": pool_name}),
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
    assert log_path == cfg / "relay-agent.p1.log"
    assert (cfg / "relay-agent.p1.pid").read_text(encoding="utf-8") == "4321\n"
    assert (
        json.loads((cfg / "relay-agent.p1.state.json").read_text())["pool_id"] == "p1"
    )

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
    assert not (cfg / "relay-agent.p1.pid").exists()
    assert not (cfg / "relay-agent.p1.state.json").exists()


def test_start_rejects_an_existing_agent(cfg, monkeypatch):
    tracked_agent(cfg)
    monkeypatch.setattr(contribution.binaries, "find", lambda name: "relay-agent-test")
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: True)

    with pytest.raises(contribution.ContributionError, match="already contributing"):
        contribution.start(SAVED, SCHED)


def test_status_reads_the_tracked_pool(cfg, monkeypatch):
    tracked_agent(cfg)
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: True)

    assert contribution.status("p1") == {
        "pid": 4321,
        "pool_id": "p1",
        "pool_name": "lab",
        "scheduler": None,
    }


def test_status_removes_stale_pid_and_state(cfg, monkeypatch):
    tracked_agent(cfg)
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: False)

    assert contribution.status("p1") is None
    assert not (cfg / "relay-agent.p1.pid").exists()
    assert not (cfg / "relay-agent.p1.state.json").exists()


def test_stop_signals_the_tracked_process(cfg, monkeypatch):
    tracked_agent(cfg)
    signals = []

    def fake_kill(pid, sig):
        signals.append((pid, sig))

    # Before the signal the process is alive; afterward it has exited.
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: not signals)
    monkeypatch.setattr(contribution.os, "kill", fake_kill)

    stopped = contribution.stop("p1")

    expected_signal = (
        signal.CTRL_BREAK_EVENT if sys.platform == "win32" else signal.SIGTERM
    )
    assert signals == [(4321, expected_signal)]
    assert stopped["pool_name"] == "lab"
    assert not (cfg / "relay-agent.p1.pid").exists()
    assert not (cfg / "relay-agent.p1.state.json").exists()


def test_two_pools_run_independently(cfg, monkeypatch):
    second = {
        **SAVED,
        "scheduler": "http://other:8080",
        "pool_id": "p2",
        "pool_name": "second",
        "pool_token": "tok2",
    }
    next_pid = iter([4321, 5678])

    class FakeProcess:
        def __init__(self):
            self.pid = next(next_pid)

        def poll(self):
            return None

    monkeypatch.setattr(contribution.binaries, "find", lambda name: "relay-agent-test")
    monkeypatch.setattr(
        contribution.subprocess, "Popen", lambda *a, **kw: FakeProcess()
    )
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: True)
    monkeypatch.setattr(contribution, "STARTUP_SECONDS", 0)

    pid1, log1 = contribution.start(SAVED, SCHED)
    pid2, log2 = contribution.start(second, second["scheduler"])

    assert (pid1, pid2) == (4321, 5678)
    assert log1 == cfg / "relay-agent.p1.log"
    assert log2 == cfg / "relay-agent.p2.log"
    assert {state["pool_id"] for state in contribution.statuses()} == {"p1", "p2"}
    assert {state["pid"] for state in contribution.statuses()} == {4321, 5678}

    with pytest.raises(contribution.ContributionError, match="already contributing"):
        contribution.start(SAVED, SCHED)


def test_stopping_one_pool_keeps_the_other(cfg, monkeypatch):
    tracked_agent(cfg, pid=4321)
    tracked_agent(cfg, pid=5678, pool_id="p2", pool_name="second")
    stopped_pids = set()
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: pid not in stopped_pids)
    monkeypatch.setattr(contribution.os, "kill", lambda pid, sig: stopped_pids.add(pid))

    assert contribution.stop("p1")["pid"] == 4321
    assert contribution.status("p1") is None
    assert contribution.status("p2")["pid"] == 5678
    assert (cfg / "relay-agent.p2.pid").exists()


def test_older_single_agent_remains_visible_and_stoppable(cfg, monkeypatch):
    tracked_agent(cfg, legacy=True)
    stopped_pids = set()
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: pid not in stopped_pids)
    monkeypatch.setattr(contribution.os, "kill", lambda pid, sig: stopped_pids.add(pid))

    assert contribution.status("p1")["pid"] == 4321
    assert [state["pid"] for state in contribution.statuses()] == [4321]
    assert contribution.stop("p1")["pid"] == 4321
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
    profile.save(SAVED)
    state = {"pid": 4321, "pool_name": "lab", "pool_id": "p1"}
    monkeypatch.setattr(contribution, "statuses", lambda: [state])
    monkeypatch.setattr(
        contribution, "stop", lambda pool_id: state if pool_id == "p1" else None
    )

    contribute.run_status(argparse.Namespace())
    assert "contributing to lab (p1)" in capsys.readouterr().out

    contribute.run_stop(argparse.Namespace())
    assert "stopped contributing to lab (p1)" in capsys.readouterr().out


def test_cli_named_pool_uses_its_scheduler_without_switching(cfg, monkeypatch, capsys):
    profile.save(SAVED)
    profile.save(SECOND)
    started = []

    def fake_start(prof, scheduler):
        started.append((prof["pool_id"], scheduler, prof["pool_token"]))
        return 4321, cfg / "relay-agent.p1.log"

    monkeypatch.setattr(contribution, "start", fake_start)
    contribute.run_start(
        argparse.Namespace(
            scheduler=SECOND["scheduler"], pool="lab", scheduler_given=False
        )
    )

    assert started == [("p1", SCHED, "tok")]
    assert profile.load()["pool_id"] == "p2"
    assert "contributing to lab" in capsys.readouterr().out


def test_cli_explicit_scheduler_overrides_selected_pool(cfg, monkeypatch):
    profile.save(SAVED)
    started = []
    monkeypatch.setattr(
        contribution,
        "start",
        lambda prof, scheduler: (started.append(scheduler) or 4321, cfg / "agent.log"),
    )
    contribute.run_start(
        argparse.Namespace(
            scheduler="http://override:8080", scheduler_given=True, pool="lab"
        )
    )
    assert started == ["http://override:8080"]


def test_cli_status_lists_every_pool(cfg, monkeypatch, capsys):
    states = [
        {"pid": 4321, "pool_name": "lab", "pool_id": "p1", "scheduler": SCHED},
        {
            "pid": 5678,
            "pool_name": "second",
            "pool_id": "p2",
            "scheduler": SECOND["scheduler"],
        },
    ]
    monkeypatch.setattr(contribution, "statuses", lambda: states)
    contribute.run_status(argparse.Namespace())
    output = capsys.readouterr().out
    assert "contributing to lab (p1) (PID 4321) at http://test:8080" in output
    assert "contributing to second (p2) (PID 5678) at http://other:8080" in output


def test_cli_stop_named_pool_without_changing_current(cfg, monkeypatch, capsys):
    profile.save(SAVED)
    profile.save(SECOND)
    tracked_agent(cfg, pid=4321)
    tracked_agent(cfg, pid=5678, pool_id="p2", pool_name="second")
    stopped = set()
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: pid not in stopped)
    monkeypatch.setattr(contribution.os, "kill", lambda pid, sig: stopped.add(pid))

    contribute.run_stop(argparse.Namespace(pool="lab", all=False))

    assert stopped == {4321}
    assert contribution.status("p2")["pid"] == 5678
    assert profile.load()["pool_id"] == "p2"
    assert "stopped contributing to lab (p1)" in capsys.readouterr().out


def test_cli_stop_all_includes_agents_without_saved_logins(cfg, monkeypatch, capsys):
    tracked_agent(cfg, pid=4321)
    tracked_agent(cfg, pid=5678, pool_id="p2", pool_name="second")
    stopped = set()
    monkeypatch.setattr(contribution, "_pid_alive", lambda pid: pid not in stopped)
    monkeypatch.setattr(contribution.os, "kill", lambda pid, sig: stopped.add(pid))

    contribute.run_stop(argparse.Namespace(all=True))

    assert stopped == {4321, 5678}
    assert contribution.statuses() == []
    output = capsys.readouterr().out
    assert "stopped contributing to lab (p1)" in output
    assert "stopped contributing to second (p2)" in output


def test_cli_unknown_or_ambiguous_pool_is_reported(cfg, monkeypatch, capsys):
    profile.save(SAVED)
    profile.save({**SECOND, "pool_name": "lab"})
    monkeypatch.setattr(contribution, "start", lambda *args: pytest.fail("started"))

    with pytest.raises(SystemExit) as error:
        contribute.run_start(argparse.Namespace(scheduler=SCHED, pool="lab"))
    assert error.value.code == 1
    assert "ambiguous" in capsys.readouterr().err

    with pytest.raises(SystemExit) as error:
        contribute.run_start(argparse.Namespace(scheduler=SCHED, pool="missing"))
    assert error.value.code == 1
    assert "not saved" in capsys.readouterr().err


def test_cli_contribute_argument_parsing():
    parser = argparse.ArgumentParser()
    contribute.register(parser.add_subparsers(dest="command"))
    assert parser.parse_args(["contribute", "--pool", "lab"]).pool == "lab"
    assert parser.parse_args(["contribute", "status"]).func == contribute.run_status
    args = parser.parse_args(["contribute", "stop", "--pool", "lab"])
    assert args.pool == "lab" and args.all is False
    args = parser.parse_args(["contribute", "stop", "--all"])
    assert args.all is True
    with pytest.raises(SystemExit):
        parser.parse_args(["contribute", "stop", "--all", "--pool", "lab"])


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
