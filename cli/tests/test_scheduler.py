import argparse

import pytest

from relay import procs, profile
from relay.commands import pool, scheduler


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    (tmp_path / "relay").mkdir()
    monkeypatch.setenv("RELAYD", str(tmp_path / "relayd"))
    monkeypatch.delenv("RELAY_ADMIN_TOKEN", raising=False)
    monkeypatch.delenv("TS_AUTHKEY", raising=False)
    return tmp_path / "relay"


@pytest.fixture
def fake_relayd(monkeypatch):
    """procs.start writes what relayd would print and pretends to be alive."""
    calls = []
    alive = {"pid": None}

    def start(name, argv, env=None):
        calls.append((name, argv, env))
        lines = ["relayd listening on :" + argv[argv.index("--port") + 1]]
        if "--public" in argv:
            lines.append("relayd public address: https://relay.tail1234.ts.net")
        if not (profile.config_dir() / "seen-once").exists():
            lines.insert(0, "server admin token: sat123")
            (profile.config_dir() / "seen-once").write_text("")
        with open(procs.log_path(name), "a") as f:
            f.write("\n".join(lines) + "\n")
        alive["pid"] = 4242
        return 4242

    monkeypatch.setattr(procs, "start", start)
    monkeypatch.setattr(procs, "running", lambda name: alive["pid"])
    monkeypatch.setattr(
        procs, "stop", lambda name, timeout=5.0: alive.pop("pid", None) is not None
    )
    return calls


def ns(**kw):
    base = {"public": False, "port": 8080, "name": "relay"}
    base.update(kw)
    return argparse.Namespace(**base)


def test_start_saves_token_and_addresses(cfg, fake_relayd, capsys):
    scheduler.run_start(ns())
    out = capsys.readouterr().out
    assert "relayd running (pid 4242)" in out
    assert "http://localhost:8080" in out
    assert "server admin token saved" in out
    assert profile.server()["admin_token"] == "sat123"
    assert profile.server()["address"] == "http://localhost:8080"
    argv = fake_relayd[0][1]
    assert argv[0].endswith("relayd") and argv[1:] == ["--port", "8080"]
    # pool create now needs no flag
    assert pool.server_admin_token(None) == "sat123"


def test_start_public_prompts_once_for_the_authkey(
    cfg, fake_relayd, monkeypatch, capsys
):
    monkeypatch.setattr(scheduler.getpass, "getpass", lambda prompt: "tskey-auth-abc")
    scheduler.run_start(ns(public=True))
    out = capsys.readouterr().out
    assert "Generate auth key" in out
    assert "https://relay.tail1234.ts.net" in out
    assert fake_relayd[0][2]["TS_AUTHKEY"] == "tskey-auth-abc"
    assert profile.server()["ts_authkey"] == "tskey-auth-abc"
    assert profile.server()["public_url"] == "https://relay.tail1234.ts.net"
    # second start: already running, no prompt
    monkeypatch.setattr(
        scheduler.getpass, "getpass", lambda prompt: pytest.fail("asked again")
    )
    scheduler.run_start(ns(public=True))
    assert "already running" in capsys.readouterr().out


def test_start_without_binary_says_install(cfg, monkeypatch, tmp_path):
    monkeypatch.delenv("RELAYD")
    monkeypatch.setenv("PATH", str(tmp_path))
    monkeypatch.setattr(procs, "running", lambda name: None)
    with pytest.raises(SystemExit) as exc:
        scheduler.run_start(ns())
    assert "run relay install first" in str(exc.value)


def test_start_reports_a_relayd_that_died(cfg, monkeypatch):
    def start(name, argv, env=None):
        with open(procs.log_path(name), "a") as f:
            f.write("flag provided but not defined: -bogus\n")
        return 1

    monkeypatch.setattr(procs, "start", start)
    monkeypatch.setattr(procs, "running", lambda name: None)
    with pytest.raises(SystemExit) as exc:
        scheduler.run_start(ns())
    assert "stopped right away" in str(exc.value) and "-bogus" in str(exc.value)


def test_stop_and_status(cfg, fake_relayd, capsys):
    scheduler.run_status(ns())
    assert "not running" in capsys.readouterr().out
    scheduler.run_start(ns())
    capsys.readouterr()
    scheduler.run_status(ns())
    out = capsys.readouterr().out
    assert "running (pid 4242)" in out and "admin token:     saved" in out
    scheduler.run_stop(ns())
    assert "relayd stopped" in capsys.readouterr().out
    scheduler.run_stop(ns())
    assert "not running" in capsys.readouterr().out


def test_resolve_falls_back_to_this_machines_scheduler():
    from relay.__main__ import resolve

    def pick(prof, server):
        args = argparse.Namespace(scheduler=None)
        resolve(args, {}, prof, server)
        return args.scheduler

    assert (
        pick({}, {"address": "http://localhost:9090", "public_url": "https://r.ts.net"})
        == "https://r.ts.net"
    )
    assert pick({}, {"address": "http://localhost:9090"}) == "http://localhost:9090"
    assert (
        pick({"scheduler": "http://login"}, {"public_url": "https://r.ts.net"})
        == "http://login"
    )
