import argparse

import pytest

from relay import procs, profile, services

from relay.commands import pool, scheduler


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    monkeypatch.setattr(services, "installed", lambda: False)
    (tmp_path / "relay").mkdir()
    monkeypatch.setenv("RELAYD", str(tmp_path / "relayd"))
    monkeypatch.delenv("RELAY_ADMIN_TOKEN", raising=False)
    monkeypatch.delenv("TS_AUTHKEY", raising=False)
    return tmp_path / "relay"


@pytest.fixture(autouse=True)
def no_warm_up(monkeypatch):
    """Never hit the network from tests; record the warm-up calls instead."""
    calls = []
    monkeypatch.setattr(scheduler, "warm_up", lambda url: calls.append(url) or True)
    return calls


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
    monkeypatch.setattr(procs, "running", lambda name: alive.get("pid"))
    monkeypatch.setattr(
        procs, "stop", lambda name, timeout=5.0: alive.pop("pid", None) is not None
    )
    return calls


def ns(**kw):
    base = {"public": False, "port": 8080, "name": "relay"}
    base.update(kw)
    return argparse.Namespace(**base)


def test_service_flags_are_registered():
    parser = argparse.ArgumentParser()
    scheduler.register(parser.add_subparsers(dest="command", required=True))
    start = parser.parse_args(["scheduler", "start", "--service", "--public"])
    stop = parser.parse_args(["scheduler", "stop", "--service"])
    assert start.service and start.public and start.func is scheduler.run_start
    assert stop.service and stop.func is scheduler.run_stop


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


def test_second_start_does_not_wait_for_a_token_line(
    cfg, fake_relayd, monkeypatch, capsys
):
    scheduler.run_start(ns())
    scheduler.run_stop(ns())
    capsys.readouterr()
    slept = []
    monkeypatch.setattr(procs.time, "sleep", lambda s: slept.append(s))
    scheduler.run_start(ns())
    out = capsys.readouterr().out
    assert "relayd running" in out and "already saved" in out
    assert slept == []


def test_start_public_warms_the_certificate(cfg, fake_relayd, no_warm_up, monkeypatch):
    monkeypatch.setenv("TS_AUTHKEY", "tskey-auth-x")
    scheduler.run_start(ns(public=True))
    assert no_warm_up == ["https://relay.tail1234.ts.net"]


def test_start_without_public_line_returns_after_timeout(cfg, monkeypatch, capsys):
    clock = {"t": 0.0}
    monkeypatch.setattr(procs.time, "monotonic", lambda: clock["t"])
    monkeypatch.setattr(
        procs.time, "sleep", lambda s: clock.__setitem__("t", clock["t"] + s)
    )
    monkeypatch.setenv("TS_AUTHKEY", "tskey-auth-x")

    alive = {}

    def start(name, argv, env=None):
        with open(procs.log_path(name), "a") as f:
            f.write("relayd listening on :8080\n")
        alive["pid"] = 7
        return 7

    monkeypatch.setattr(procs, "start", start)
    monkeypatch.setattr(procs, "running", lambda name: alive.get("pid"))
    scheduler.run_start(ns(public=True))
    out = capsys.readouterr().out
    assert "starting relayd and joining the tailnet" in out
    assert "not up yet" in out and "still starting" in out
    assert clock["t"] >= 60


def test_ctrl_c_during_the_wait_leaves_relayd_running(cfg, monkeypatch):
    alive = {}
    monkeypatch.setattr(
        procs, "start", lambda name, argv, env=None: alive.setdefault("pid", 7)
    )
    monkeypatch.setattr(procs, "running", lambda name: alive.get("pid"))

    def interrupted(*a, **k):
        raise KeyboardInterrupt

    monkeypatch.setattr(procs, "wait_for", interrupted)
    with pytest.raises(SystemExit) as exc:
        scheduler.run_start(ns())
    assert "keeps running" in str(exc.value)


def test_status_reads_a_new_public_address_from_the_log(
    cfg, fake_relayd, monkeypatch, capsys
):
    monkeypatch.setenv("TS_AUTHKEY", "tskey-auth-x")
    profile.save_server(public_url="https://relay-1.tail1234.ts.net")
    scheduler.run_start(ns(public=True))
    capsys.readouterr()
    scheduler.run_status(ns())
    out = capsys.readouterr().out
    assert "https://relay.tail1234.ts.net" in out and "relay-1" not in out
    assert profile.server()["public_url"] == "https://relay.tail1234.ts.net"


def test_service_lifecycle_saves_settings_and_keeps_registration(
    cfg, monkeypatch, capsys
):
    binary = cfg / "relayd"
    binary.write_text("fake binary")
    monkeypatch.setenv("RELAYD", str(binary))
    monkeypatch.setenv("TS_AUTHKEY", "tskey-auth-service")
    monkeypatch.setattr(procs, "running", lambda name: None)
    service = {"installed": False, "state": "inactive", "argv": [], "calls": []}

    def append_log(first):
        lines = [
            "relayd listening on :9123",
            "relayd public address: https://new.example",
        ]
        if first:
            lines.insert(0, "server admin token: service-token")
        with procs.log_path("relayd").open("a") as log:
            log.write("\n".join(lines) + "\n")

    def install(argv, key):
        service["calls"].append(("install", argv, key))
        service.update(installed=True, state="active", argv=argv)
        append_log(True)

    def start():
        service["calls"].append(("start",))
        service["state"] = "active"
        append_log(False)

    def stop():
        service["state"] = "inactive"
        service["calls"].append(("stop",))
        return True

    def remove():
        service["installed"] = False
        service["state"] = "inactive"
        service["calls"].append(("remove",))

    monkeypatch.setattr(services, "installed", lambda: service["installed"])
    monkeypatch.setattr(services, "install", install)
    monkeypatch.setattr(services, "state", lambda: service["state"])
    monkeypatch.setattr(services, "configuration", lambda: service["argv"])
    monkeypatch.setattr(services, "log_offset", lambda: 0)
    monkeypatch.setattr(services, "token_was_captured", lambda token: False)
    monkeypatch.setattr(services, "remember_captured_token", lambda token: None)
    monkeypatch.setattr(services, "start", start)
    monkeypatch.setattr(services, "stop", stop)
    monkeypatch.setattr(services, "remove", remove)

    scheduler.run_start(ns(service=True, public=True, port=9123, name="server"))
    out = capsys.readouterr().out
    assert "service installed" in out and "https://new.example" in out
    assert "tskey-auth-service" not in out and "service-token" not in out
    assert service["calls"][0][1:] == (
        [str(binary), "--port", "9123", "--public", "--name", "server"],
        "tskey-auth-service",
    )
    assert profile.server() == {
        "ts_authkey": "tskey-auth-service",
        "address": "http://localhost:9123",
        "admin_token": "service-token",
        "public_url": "https://new.example",
    }

    scheduler.run_start(ns())
    assert "installed as a service" in capsys.readouterr().out
    assert len(service["calls"]) == 1
    scheduler.run_status(ns())
    assert "service running (installed for boot)" in capsys.readouterr().out

    scheduler.run_stop(ns())
    assert "remains registered" in capsys.readouterr().out
    assert service["installed"]
    scheduler.run_status(ns())
    assert "service stopped (installed for boot)" in capsys.readouterr().out
    scheduler.run_start(ns(service=True))
    assert "saved configuration" in capsys.readouterr().out
    assert profile.server()["address"] == "http://localhost:9123"
    assert service["calls"][-1] == ("start",)

    scheduler.run_stop(ns(service=True))
    assert "service stopped and removed" in capsys.readouterr().out
    assert not service["installed"]


def test_service_install_refuses_a_manual_scheduler(cfg, monkeypatch):
    monkeypatch.setattr(procs, "running", lambda name: 42)
    monkeypatch.setattr(services, "install", lambda *args: pytest.fail("installed"))
    with pytest.raises(SystemExit, match="stop before installing"):
        scheduler.run_start(ns(service=True))


def test_service_install_error_is_reported(cfg, monkeypatch):
    binary = cfg / "relayd"
    binary.write_text("fake binary")
    monkeypatch.setenv("RELAYD", str(binary))
    monkeypatch.setattr(procs, "running", lambda name: None)

    def fail(*args):
        raise services.ServiceError("manager unavailable")

    monkeypatch.setattr(services, "install", fail)
    with pytest.raises(SystemExit, match="manager unavailable"):
        scheduler.run_start(ns(service=True))
