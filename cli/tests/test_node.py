import argparse

import httpx
import pytest
from relay.commands import node, pool


@pytest.fixture(autouse=True)
def no_login(monkeypatch):
    """Tests that pass --pool explicitly don't need a saved login."""
    monkeypatch.setattr(pool.profile, "load", dict)


_RealClient = httpx.Client


def mock_client(handler):
    return _RealClient(transport=httpx.MockTransport(handler))


def remove_args(**kw):
    defaults = {
        "scheduler": "http://test",
        "node_id": "n1",
        "admin_token": "admintok",
        "pool": "pool1",
        "yes": False,
    }
    defaults.update(kw)
    return argparse.Namespace(**defaults)


def test_remove_with_yes_sends_bearer(monkeypatch, capsys):
    seen = []

    def handler(req):
        seen.append((req.method, str(req.url), req.headers.get("Authorization")))
        return httpx.Response(204)

    monkeypatch.setattr(node.httpx, "Client", lambda **kw: mock_client(handler))

    node.run_remove(remove_args(yes=True))

    assert seen == [("DELETE", "http://test/pools/pool1/nodes/n1", "Bearer admintok")]
    assert "removed machine n1 from pool pool1" in capsys.readouterr().out


def test_remove_aborts_on_wrong_name(monkeypatch):
    methods = []

    def handler(req):
        methods.append(req.method)
        return httpx.Response(204)

    monkeypatch.setattr(node.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "pool1", "pool_name": "lab"}
    )
    monkeypatch.setattr("builtins.input", lambda _: "wrong")

    with pytest.raises(SystemExit):
        node.run_remove(remove_args())

    # The name comes from the login, so no request was made at all.
    assert methods == []


def test_remove_confirmed_by_name(monkeypatch):
    methods = []

    def handler(req):
        methods.append(req.method)
        return httpx.Response(204)

    monkeypatch.setattr(node.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "pool1", "pool_name": "lab"}
    )
    monkeypatch.setattr("builtins.input", lambda _: "lab")

    node.run_remove(remove_args())
    assert methods == ["DELETE"]


def test_remove_uses_env_admin_token_and_profile_pool(monkeypatch):
    seen = {}

    def handler(req):
        seen["url"] = str(req.url)
        seen["auth"] = req.headers.get("Authorization")
        return httpx.Response(204)

    monkeypatch.setattr(node.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setenv("RELAY_ADMIN_TOKEN", "envtok")
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "frompro", "pool_name": "lab"}
    )

    node.run_remove(remove_args(admin_token=None, pool=None, yes=True))
    assert seen["url"] == "http://test/pools/frompro/nodes/n1"
    assert seen["auth"] == "Bearer envtok"


def test_remove_requires_admin_token(monkeypatch):
    monkeypatch.delenv("RELAY_ADMIN_TOKEN", raising=False)
    with pytest.raises(SystemExit, match="admin token is required"):
        node.run_remove(remove_args(admin_token=None, yes=True))


def test_remove_rejected_token_is_friendly(monkeypatch):
    monkeypatch.setattr(
        node.httpx, "Client", lambda **kw: mock_client(lambda req: httpx.Response(401))
    )
    with pytest.raises(SystemExit, match="admin token was rejected"):
        node.run_remove(remove_args(yes=True))
