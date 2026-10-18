import argparse
import json

import httpx
import pytest

from relay.commands import pool
from relay.commands.pool import create

_RealClient = httpx.Client


def mock_client(handler):
    return _RealClient(transport=httpx.MockTransport(handler))


def test_rotate_sends_bearer_and_returns_token():
    seen = {}

    def handler(req):
        seen["auth"] = req.headers.get("Authorization")
        seen["url"] = str(req.url)
        return httpx.Response(200, json={"token": "newtok"})

    tok = pool.rotate_token(
        "http://test", "pool1", "admintok", client=mock_client(handler)
    )
    assert tok == "newtok"
    assert seen["auth"] == "Bearer admintok"
    assert seen["url"] == "http://test/pools/pool1/token"


def test_rotate_aborts_on_wrong_name(monkeypatch):
    methods = []

    def handler(req):
        methods.append(req.method)
        return httpx.Response(200, json=[{"id": "pool1", "name": "lab"}])

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr("builtins.input", lambda _: "wrong")

    args = argparse.Namespace(
        scheduler="http://test", pool="pool1", admin_token="x", yes=False
    )
    with pytest.raises(SystemExit):
        pool.run_rotate_token(args)
    assert methods == ["GET"]


def test_create_prints_real_agent_command(monkeypatch, capsys):
    def handler(req):
        return httpx.Response(
            201, json={"id": "p1", "name": "lab", "token": "jt", "admin_token": "at"}
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    pool.run_create(argparse.Namespace(scheduler="http://test", name="lab"))
    out = capsys.readouterr().out
    assert "relay-agent --scheduler http://test --pool-token jt" in out
    assert "admin token: at" in out


def test_create_returns_pool():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/pools"
        assert json.loads(request.content)["name"] == "csc4996"
        return httpx.Response(200, json={"id": "pool-123", "token": "tok-abc"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    pool = create("http://scheduler", "csc4996", client=client)
    assert pool["id"] == "pool-123"
    assert pool["token"] == "tok-abc"
