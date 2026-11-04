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
        return httpx.Response(200, json={"token": "newtok"})

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "pool1", "pool_name": "lab"}
    )
    monkeypatch.setattr("builtins.input", lambda _: "wrong")

    args = argparse.Namespace(
        scheduler="http://test", pool="pool1", admin_token="x", yes=False
    )
    with pytest.raises(SystemExit):
        pool.run_rotate_token(args)
    # The name to confirm comes from the login, so GET /pools is never called.
    assert methods == []


def test_create_prints_real_agent_command(monkeypatch, capsys):
    def handler(req):
        return httpx.Response(
            201, json={"id": "p1", "name": "lab", "token": "jt", "admin_token": "at"}
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    pool.run_create(
        argparse.Namespace(scheduler="http://test", name="lab", admin_token="sat")
    )
    out = capsys.readouterr().out
    assert "relay-agent --scheduler http://test --pool-token jt" in out
    assert "admin token: at" in out


def test_create_returns_pool():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/pools"
        assert json.loads(request.content)["name"] == "csc4996"
        assert request.headers["Authorization"] == "Bearer sat"
        return httpx.Response(200, json={"id": "pool-123", "token": "tok-abc"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    pool = create("http://scheduler", "csc4996", "sat", client=client)
    assert pool["id"] == "pool-123"
    assert pool["token"] == "tok-abc"


INVITE_CASES = [
    pytest.param(
        "http://localhost:8080",
        "http://<your address>:8080",
        True,
        id="localhost",
    ),
    pytest.param(
        "http://127.0.0.1:9000",
        "http://<your address>:9000",
        True,
        id="ipv4-loopback",
    ),
    pytest.param(
        "http://[::1]:8080",
        "http://<your address>:8080",
        True,
        id="ipv6-loopback",
    ),
    pytest.param(
        "http://10.1.4.3:8888",
        "http://10.1.4.3:8888",
        False,
        id="remote-ipv4",
    ),
]


@pytest.mark.parametrize("scheduler, expected_invite, needs_hint", INVITE_CASES)
def test_invite_scheduler_url(scheduler, expected_invite, needs_hint):
    assert pool.invite_scheduler_url(scheduler) == (expected_invite, needs_hint)


@pytest.mark.parametrize(
    "scheduler, expected_invite, needs_hint",
    [
        pytest.param(
            "http://127.1.2.3:8080",
            "http://<your address>:8080",
            True,
            id="other-ipv4-loopback",
        ),
        pytest.param(
            "https://localhost:8443",
            "https://<your address>:8443",
            True,
            id="https-loopback",
        ),
        pytest.param(
            "http://scheduler.tailnet.ts.net:8080",
            "http://scheduler.tailnet.ts.net:8080",
            False,
            id="tailscale-host",
        ),
    ],
)
def test_invite_scheduler_url_additional_hosts(scheduler, expected_invite, needs_hint):
    assert pool.invite_scheduler_url(scheduler) == (expected_invite, needs_hint)


@pytest.mark.parametrize("scheduler, expected_invite, needs_hint", INVITE_CASES)
def test_create_invite_handles_scheduler_addresses(
    monkeypatch, capsys, scheduler, expected_invite, needs_hint
):
    requests = []

    def handler(req):
        requests.append((str(req.url), req.headers.get("Authorization")))
        return httpx.Response(
            201, json={"id": "p1", "name": "lab", "token": "jt", "admin_token": "at"}
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    pool.run_create(
        argparse.Namespace(scheduler=scheduler, name="lab", admin_token="sat")
    )

    out = capsys.readouterr().out
    assert requests == [(f"{scheduler}/pools", "Bearer sat")]
    assert f"relay-agent --scheduler {expected_invite} --pool-token jt" in out
    assert ("replace <your address>" in out) is needs_hint
    assert ("Tailscale name" in out) is needs_hint


@pytest.mark.parametrize("scheduler, expected_invite, needs_hint", INVITE_CASES)
def test_rotate_invite_handles_scheduler_addresses(
    monkeypatch, capsys, scheduler, expected_invite, needs_hint
):
    requests = []

    def handler(req):
        requests.append((str(req.url), req.headers.get("Authorization")))
        return httpx.Response(200, json={"token": "newtok"})

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "p1", "pool_name": "lab"}
    )
    pool.run_rotate_token(
        argparse.Namespace(scheduler=scheduler, pool="p1", admin_token="at", yes=True)
    )

    out = capsys.readouterr().out
    assert requests == [(f"{scheduler}/pools/p1/token", "Bearer at")]
    assert f"relay-agent --scheduler {expected_invite} --pool-token newtok" in out
    assert ("replace <your address>" in out) is needs_hint
    assert ("Tailscale name" in out) is needs_hint


# #113: with relayd --public, the API returns public_url and the invite uses
# it as-is, with no "<your address>" hint, whatever --scheduler the admin used.
PUBLIC_URL = "https://relay.tail1234.ts.net"


@pytest.mark.parametrize("scheduler, _invite, _hint", INVITE_CASES)
def test_create_invite_uses_public_url(monkeypatch, capsys, scheduler, _invite, _hint):
    def handler(req):
        return httpx.Response(
            201,
            json={
                "id": "p1",
                "name": "lab",
                "token": "jt",
                "admin_token": "at",
                "public_url": PUBLIC_URL,
            },
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    pool.run_create(
        argparse.Namespace(scheduler=scheduler, name="lab", admin_token="sat")
    )

    out = capsys.readouterr().out
    assert f"relay-agent --scheduler {PUBLIC_URL} --pool-token jt" in out
    assert "<your address>" not in out
    assert "Tailscale name" not in out


@pytest.mark.parametrize("scheduler, _invite, _hint", INVITE_CASES)
def test_rotate_invite_uses_public_url(monkeypatch, capsys, scheduler, _invite, _hint):
    def handler(req):
        return httpx.Response(200, json={"token": "newtok", "public_url": PUBLIC_URL})

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "p1", "pool_name": "lab"}
    )
    pool.run_rotate_token(
        argparse.Namespace(scheduler=scheduler, pool="p1", admin_token="at", yes=True)
    )

    out = capsys.readouterr().out
    assert f"relay-agent --scheduler {PUBLIC_URL} --pool-token newtok" in out
    assert "<your address>" not in out
    assert "Tailscale name" not in out


@pytest.mark.parametrize("scheduler, expected_invite, needs_hint", INVITE_CASES)
def test_create_invite_empty_public_url_unchanged(
    monkeypatch, capsys, scheduler, expected_invite, needs_hint
):
    def handler(req):
        return httpx.Response(
            201,
            json={
                "id": "p1",
                "name": "lab",
                "token": "jt",
                "admin_token": "at",
                "public_url": "",
            },
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    pool.run_create(
        argparse.Namespace(scheduler=scheduler, name="lab", admin_token="sat")
    )

    out = capsys.readouterr().out
    assert f"relay-agent --scheduler {expected_invite} --pool-token jt" in out
    assert ("replace <your address>" in out) is needs_hint


@pytest.mark.parametrize("scheduler, expected_invite, needs_hint", INVITE_CASES)
def test_rotate_invite_empty_public_url_unchanged(
    monkeypatch, capsys, scheduler, expected_invite, needs_hint
):
    def handler(req):
        return httpx.Response(200, json={"token": "newtok", "public_url": ""})

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile, "load", lambda: {"pool_id": "p1", "pool_name": "lab"}
    )
    pool.run_rotate_token(
        argparse.Namespace(scheduler=scheduler, pool="p1", admin_token="at", yes=True)
    )

    out = capsys.readouterr().out
    assert f"relay-agent --scheduler {expected_invite} --pool-token newtok" in out
    assert ("replace <your address>" in out) is needs_hint


def test_rotate_returns_public_url():
    def handler(req):
        assert str(req.url) == "http://test/pools/pool1/token"
        assert req.headers.get("Authorization") == "Bearer admintok"
        return httpx.Response(200, json={"token": "newtok", "public_url": PUBLIC_URL})

    body = pool.rotate("http://test", "pool1", "admintok", client=mock_client(handler))
    assert body == {"token": "newtok", "public_url": PUBLIC_URL}

def test_create_uses_this_machines_scheduler_when_flag_not_given(monkeypatch, capsys):
    seen = []

    def handler(req):
        seen.append(str(req.url))
        return httpx.Response(
            201, json={"id": "p1", "name": "lab", "token": "jt", "admin_token": "at"}
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(
        pool.profile,
        "server",
        lambda: {"address": "http://localhost:8080", "public_url": "https://me.ts.net"},
    )
    pool.run_create(
        argparse.Namespace(
            scheduler="http://friend:8080",
            scheduler_given=False,
            name="lab",
            admin_token="sat",
        )
    )
    assert seen == ["https://me.ts.net/pools"]
    assert "relay-agent --scheduler https://me.ts.net" in capsys.readouterr().out


def test_create_flag_beats_server_settings(monkeypatch):
    seen = []

    def handler(req):
        seen.append(str(req.url))
        return httpx.Response(
            201, json={"id": "p1", "name": "lab", "token": "jt", "admin_token": "at"}
        )

    monkeypatch.setattr(pool.httpx, "Client", lambda **kw: mock_client(handler))
    monkeypatch.setattr(pool.profile, "server", lambda: {"address": "http://mine:8080"})
    pool.run_create(
        argparse.Namespace(
            scheduler="http://other:8080",
            scheduler_given=True,
            name="lab",
            admin_token="sat",
        )
    )
    assert seen == ["http://other:8080/pools"]


def test_create_without_server_settings_or_flag_exits(monkeypatch):
    monkeypatch.setattr(pool.profile, "server", dict)
    with pytest.raises(SystemExit) as exc:
        pool.run_create(
            argparse.Namespace(
                scheduler="http://friend:8080",
                scheduler_given=False,
                name="lab",
                admin_token="sat",
            )
        )
    assert "no scheduler on this machine" in str(exc.value)