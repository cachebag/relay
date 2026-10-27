"""#95 and #96 on the CLI side: every pool command sends the join token from
the saved login, refuses to run without one, and pool create needs the server
admin token."""

import argparse

import httpx
import pytest

from relay import profile
from relay.commands import jobs, logs, nodes, pool, status

PROF = {
    "scheduler": "http://test",
    "pool_id": "p1",
    "pool_name": "lab",
    "pool_token": "join-tok",
    "name": "alice",
}


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    """Point the profile at a temp dir so tests never touch a real login."""
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    return tmp_path / "relay" / profile.FILENAME


def route_all(monkeypatch, module, handler):
    """Send every httpx.Client the module makes through handler."""
    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(module.httpx, "Client", lambda **kw: client)


COMMANDS = [
    ("nodes", nodes, argparse.Namespace(scheduler="http://test"), "/nodes"),
    ("jobs", jobs, argparse.Namespace(scheduler="http://test"), "/jobs"),
    (
        "status",
        status,
        argparse.Namespace(scheduler="http://test", job_id="j1"),
        "/jobs/j1",
    ),
    (
        "logs",
        logs,
        argparse.Namespace(scheduler="http://test", id="j1"),
        "/jobs/j1/output",
    ),
]


@pytest.mark.parametrize(
    "name,module,args,path", COMMANDS, ids=[c[0] for c in COMMANDS]
)
def test_commands_send_bearer_from_profile(cfg, monkeypatch, name, module, args, path):
    profile.save(PROF)
    seen = {}

    def handler(req):
        seen["path"] = req.url.path
        seen["auth"] = req.headers.get("Authorization")
        if path.endswith("/output"):
            return httpx.Response(
                200, json={"stdout": "", "stderr": "", "exit_code": 0}
            )
        if path == "/jobs/j1":
            return httpx.Response(
                200,
                json={
                    "id": "j1",
                    "status": "queued",
                    "node_id": "",
                    "attempts": 0,
                    "command": ["x"],
                },
            )
        return httpx.Response(200, json=[])

    route_all(monkeypatch, module, handler)

    try:
        module.run(args)
    except SystemExit as exc:
        # logs exits with the job's exit code, which is 0 here.
        assert exc.code == 0

    assert seen["path"] == path
    assert seen["auth"] == "Bearer join-tok"


@pytest.mark.parametrize(
    "name,module,args,path", COMMANDS, ids=[c[0] for c in COMMANDS]
)
def test_commands_need_login(cfg, monkeypatch, capsys, name, module, args, path):
    calls = []
    route_all(monkeypatch, module, lambda req: calls.append(req) or httpx.Response(200))

    with pytest.raises(SystemExit) as exc:
        module.run(args)

    assert exc.value.code == 1
    assert "not logged in, run relay login first" in capsys.readouterr().err
    assert calls == []


def test_pool_create_sends_server_admin_token(monkeypatch, capsys):
    seen = {}

    def handler(req):
        seen["auth"] = req.headers.get("Authorization")
        return httpx.Response(
            201, json={"id": "p9", "name": "x", "token": "jt", "admin_token": "at"}
        )

    route_all(monkeypatch, pool, handler)
    pool.run_create(
        argparse.Namespace(scheduler="http://test", name="x", admin_token="sat")
    )
    assert seen["auth"] == "Bearer sat"
    assert "pool id:    p9" in capsys.readouterr().out


def test_pool_create_without_token_exits_1(cfg, monkeypatch, capsys):
    monkeypatch.delenv("RELAY_ADMIN_TOKEN", raising=False)
    calls = []
    route_all(monkeypatch, pool, lambda req: calls.append(req) or httpx.Response(201))

    with pytest.raises(SystemExit) as exc:
        pool.run_create(
            argparse.Namespace(scheduler="http://test", name="x", admin_token=None)
        )

    assert exc.value.code == 1
    assert pool.ADMIN_TOKEN_HINT in capsys.readouterr().err
    assert calls == []


def test_pool_create_uses_env_token(monkeypatch):
    monkeypatch.setenv("RELAY_ADMIN_TOKEN", "from-env")
    seen = {}

    def handler(req):
        seen["auth"] = req.headers.get("Authorization")
        return httpx.Response(
            201, json={"id": "p9", "name": "x", "token": "jt", "admin_token": "at"}
        )

    route_all(monkeypatch, pool, handler)
    pool.run_create(
        argparse.Namespace(scheduler="http://test", name="x", admin_token=None)
    )
    assert seen["auth"] == "Bearer from-env"


def test_rotate_token_uses_login_pool_and_makes_no_get(cfg, monkeypatch, capsys):
    """rotate-token takes the pool and the name to confirm from the login."""
    profile.save(PROF)
    requests = []

    def handler(req):
        requests.append((req.method, req.url.path))
        return httpx.Response(200, json={"token": "newtok"})

    route_all(monkeypatch, pool, handler)
    monkeypatch.setattr("builtins.input", lambda prompt: "lab")

    pool.run_rotate_token(
        argparse.Namespace(
            scheduler="http://test", pool=None, admin_token="at", yes=False
        )
    )

    assert requests == [("POST", "/pools/p1/token")]
    assert "new join token: newtok" in capsys.readouterr().out


def test_rotate_token_without_login_or_pool_exits(cfg, monkeypatch):
    route_all(monkeypatch, pool, lambda req: httpx.Response(200, json={"token": "x"}))
    with pytest.raises(SystemExit, match="no pool selected"):
        pool.run_rotate_token(
            argparse.Namespace(
                scheduler="http://test", pool=None, admin_token="at", yes=True
            )
        )
