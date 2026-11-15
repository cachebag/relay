import argparse

import pytest

from relay import contribution, procs, profile, services
from relay.commands import reset


@pytest.fixture
def machine(tmp_path, monkeypatch):
    """A config folder and agent cache with everything relay leaves behind."""
    config = tmp_path / "config" / "relay"
    cache = tmp_path / "cache" / "relay"
    monkeypatch.setattr(profile, "config_dir", lambda: config)
    monkeypatch.setattr(
        contribution, "_agent_cache_dir", lambda pool_id: cache / "agents" / pool_id
    )
    monkeypatch.setattr(contribution, "stop_all", list)
    monkeypatch.setattr(services, "installed", lambda: False)
    monkeypatch.setattr(procs, "stop", lambda name: False)

    (config / "bin").mkdir(parents=True)
    (config / "bin" / "relayd").write_text("binary")
    (config / "tsnet").mkdir()
    (config / "tsnet" / "tailscaled.state").write_text("identity")
    (config / "relay.db").write_text("pools")
    (config / "relayd.log").write_text("log")
    (config / "relay-agent.p1.pid").write_text("123")
    profile.save({"scheduler": "http://a", "pool_id": "p1", "pool_token": "t"})
    profile.save_server(admin_token="at", ts_authkey="tskey-auth-k")
    (cache / "agents" / "p1" / "jobs").mkdir(parents=True)
    (cache / "node-id").write_text("n1\n")
    return config, cache


def run(tailscale=False):
    reset.run(argparse.Namespace(yes=True, tailscale=tailscale))


def test_reset_wipes_data_and_keeps_bin_tsnet_and_key(machine, capsys):
    config, cache = machine
    run()
    assert sorted(p.name for p in config.iterdir()) == ["bin", "cli.json", "tsnet"]
    assert (config / "bin" / "relayd").read_text() == "binary"
    assert (config / "tsnet" / "tailscaled.state").read_text() == "identity"
    assert profile.logins() == []
    assert profile.server() == {"ts_authkey": "tskey-auth-k"}
    assert not cache.exists()
    assert "relay reset done" in capsys.readouterr().out


def test_reset_tailscale_also_forgets_identity_and_key(machine):
    config, _ = machine
    run(tailscale=True)
    assert [p.name for p in config.iterdir()] == ["bin"]
    assert profile.server() == {}


def test_reset_stops_agents_and_relayd_first(machine, monkeypatch, capsys):
    config, _ = machine
    calls = []

    def stop_all():
        calls.append("agents")
        assert (config / "relay.db").exists()
        return [{"pid": 1, "pool_id": "p1", "pool_name": "lab"}]

    def stop(name):
        calls.append(name)
        return True

    monkeypatch.setattr(contribution, "stop_all", stop_all)
    monkeypatch.setattr(procs, "stop", stop)
    run()
    assert calls == ["agents", "relayd"]
    out = capsys.readouterr().out
    assert "stopped contributing to lab" in out
    assert "stopped relayd" in out


def test_reset_keeps_going_when_relayd_cannot_be_checked(machine, monkeypatch, capsys):
    config, _ = machine

    def stop(name):
        raise OSError(87, "The parameter is incorrect")

    monkeypatch.setattr(procs, "stop", stop)
    run()
    assert not (config / "relay.db").exists()
    assert "could not stop relayd" in capsys.readouterr().err


def test_reset_asks_first_and_can_be_cancelled(machine, monkeypatch, capsys):
    config, _ = machine
    monkeypatch.setattr("builtins.input", lambda prompt: "n")
    reset.run(argparse.Namespace(yes=False, tailscale=False))
    assert (config / "relay.db").exists()
    assert "reset cancelled" in capsys.readouterr().out


def test_reset_on_a_fresh_machine_does_nothing_harmful(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "none")
    monkeypatch.setattr(
        contribution,
        "_agent_cache_dir",
        lambda p: tmp_path / "c" / "relay" / "agents" / p,
    )
    monkeypatch.setattr(contribution, "stop_all", list)
    monkeypatch.setattr(services, "installed", lambda: False)
    monkeypatch.setattr(procs, "stop", lambda name: False)
    run()
    assert not (tmp_path / "none").exists()
