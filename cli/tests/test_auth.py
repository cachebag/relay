import argparse
import json
import sys

import httpx
import pytest

from relay import profile
from relay.__main__ import DEFAULT_SCHEDULER, resolve
from relay.commands import auth


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    return tmp_path / "relay" / profile.FILENAME


def client(status: int, body: dict | None = None):
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/pools/self"
        assert request.headers["Authorization"] == "Bearer tok"
        return httpx.Response(status, json=body or {})

    return httpx.Client(transport=httpx.MockTransport(handler))


POOL = {"id": "961dda5133b6", "name": "lab", "created_at": "2026-01-01T00:00:00Z"}


def test_login_writes_profile(cfg):
    prof = auth.login("http://localhost:9090", "tok", "omar", client(200, POOL))
    want = {
        "scheduler": "http://localhost:9090",
        "pool_id": "961dda5133b6",
        "pool_name": "lab",
        "pool_token": "tok",
        "name": "omar",
    }
    assert prof == profile.load() == want
    # The file holds a list of logins and which one is current.
    assert json.loads(cfg.read_text()) == {"current": "961dda5133b6", "logins": [want]}
    if sys.platform != "win32":
        assert cfg.stat().st_mode & 0o777 == 0o600


def test_login_invalid_token_writes_nothing(cfg):
    with pytest.raises(httpx.HTTPStatusError):
        auth.login("http://localhost:8080", "tok", "omar", client(403))
    assert not cfg.exists()


def test_run_login_invalid_token_exits_1(cfg, monkeypatch, capsys):
    transport_client = client(403)
    monkeypatch.setattr(httpx, "Client", lambda **kw: transport_client)
    args = argparse.Namespace(
        scheduler="http://localhost:9090",
        scheduler_given=True,
        pool_token="tok",
        name="omar",
    )
    with pytest.raises(SystemExit) as e:
        auth.run_login(args)
    assert e.value.code == 1
    assert "invalid pool token" in capsys.readouterr().err
    assert not cfg.exists()


def test_logout_removes_file(cfg):
    profile.save({"name": "omar"})
    auth.run_logout(argparse.Namespace())
    assert not cfg.exists()
    auth.run_logout(argparse.Namespace())  # idempotent


def test_whoami_not_logged_in(cfg, capsys):
    with pytest.raises(SystemExit) as e:
        auth.run_whoami(argparse.Namespace())
    assert e.value.code == 1
    assert "not logged in" in capsys.readouterr().out


def test_whoami_prints_profile(cfg, capsys):
    auth.login("http://localhost:9090", "tok", "omar", client(200, POOL))
    auth.run_whoami(argparse.Namespace())
    out = capsys.readouterr().out
    assert "omar" in out and "lab" in out and "http://localhost:9090" in out
    assert "tok" not in out.replace("http://", "")


def test_pools_and_use(cfg, capsys):
    auth.login("http://localhost:9090", "tok", "omar", client(200, POOL))
    auth.login(
        "http://other:9090",
        "tok",
        "omar",
        client(200, {**POOL, "id": "p2", "name": "home"}),
    )
    auth.run_pools(argparse.Namespace())
    out = capsys.readouterr().out
    assert "  lab " in out and "* home " in out
    auth.run_use(argparse.Namespace(pool="lab"))
    assert "now using pool lab" in capsys.readouterr().out
    assert profile.load()["pool_name"] == "lab"
    with pytest.raises(SystemExit):
        auth.run_use(argparse.Namespace(pool="nope"))
    assert "no saved login for nope" in capsys.readouterr().err


def test_logout_moves_to_the_next_pool(cfg, capsys):
    auth.login("http://localhost:9090", "tok", "omar", client(200, POOL))
    auth.login(
        "http://other:9090",
        "tok",
        "omar",
        client(200, {**POOL, "id": "p2", "name": "home"}),
    )
    auth.run_logout(argparse.Namespace())
    assert "now using pool lab" in capsys.readouterr().out
    assert profile.load()["pool_name"] == "lab"


def ns(scheduler=None, subcommand="nodes", pool=None):
    return argparse.Namespace(scheduler=scheduler, subcommand=subcommand, pool=pool)


PROF = {"scheduler": "http://profile", "pool_id": "p1"}


def test_scheduler_precedence():
    env = {"RELAY_SCHEDULER": "http://env"}

    a = ns("http://flag")
    resolve(a, env, PROF)
    assert a.scheduler == "http://flag"

    a = ns()
    resolve(a, env, PROF)
    assert a.scheduler == "http://env"

    a = ns()
    resolve(a, {}, PROF)
    assert a.scheduler == "http://profile"

    a = ns()
    resolve(a, {}, {})
    assert a.scheduler == DEFAULT_SCHEDULER
