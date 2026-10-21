import asyncio
import json

import httpx
import pytest
from textual.widgets import Input

from relay import profile
from relay.dash import DashApp, invite_line, parse_invite
from relay.dash.login import LoginScreen
from relay.dash.nodes import NodesScreen
from relay.dash.pool import PoolScreen

_RealClient = httpx.Client

SCHED = "http://test:8080"
POOL = {"id": "p1", "name": "lab", "created_at": "2026-01-01T00:00:00Z"}
SAVED = {
    "scheduler": SCHED,
    "pool_id": "p1",
    "pool_name": "lab",
    "pool_token": "tok",
    "name": "abdoul",
}
NODE = {
    "id": "n1",
    "hostname": "box",
    "os": "linux",
    "arch": "amd64",
    "cpus": 4,
    "mem_bytes": 8e9,
    "last_seen": "2026-01-01T00:00:00+00:00",
    "pool_id": "p1",
    "status": "online",
}
JOB = {
    "id": "j1",
    "status": "queued",
    "node_id": "",
    "attempts": 0,
    "created_at": "2026-01-01T00:00:00+00:00",
    "command": ["uname", "-n"],
}


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    return tmp_path / "relay" / profile.FILENAME


@pytest.fixture
def calls(monkeypatch):
    """Route every httpx.Client through a MockTransport and record requests."""
    seen: list[tuple[str, str, str | None]] = []

    def handler(req: httpx.Request) -> httpx.Response:
        seen.append((req.method, req.url.path, req.headers.get("Authorization")))
        if req.url.path == "/pools/self":
            return httpx.Response(200, json=POOL)
        if req.url.path == "/pools" and req.method == "POST":
            name = json.loads(req.content)["name"]
            return httpx.Response(
                201,
                json={"id": "p2", "name": name, "token": "newjt", "admin_token": "pat"},
            )
        if req.url.path == "/nodes":
            return httpx.Response(200, json=[NODE])
        if req.url.path == "/jobs":
            return httpx.Response(200, json=[JOB])
        return httpx.Response(404)

    monkeypatch.setattr(
        httpx,
        "Client",
        lambda **kw: _RealClient(transport=httpx.MockTransport(handler)),
    )
    return seen


def drive(app, steps):
    async def run():
        async with app.run_test() as pilot:
            await steps(pilot)

    asyncio.run(run())


async def settle(pilot):
    await pilot.app.workers.wait_for_complete()
    await pilot.pause()


def test_parse_invite():
    line = invite_line("http://10.0.0.5:8080", "jt")
    assert parse_invite(line, SCHED) == ("http://10.0.0.5:8080", "jt")
    assert parse_invite("  jt  ", SCHED) == (SCHED, "jt")
    assert parse_invite("--pool-token=jt --scheduler=http://x/", SCHED) == (
        "http://x",
        "jt",
    )
    with pytest.raises(ValueError):
        parse_invite("relay-agent --scheduler http://x", SCHED)


def test_shell_starts_on_login_without_saved_login(cfg, calls):
    app = DashApp(SCHED)

    async def steps(pilot):
        assert isinstance(app.screen, LoginScreen)

    drive(app, steps)
    assert calls == []


def test_shell_starts_on_pool_with_saved_login(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        assert isinstance(app.screen, PoolScreen)

    drive(app, steps)


def test_login_screen_calls_login_with_pasted_token(cfg, calls):
    app = DashApp("http://ignored")

    async def steps(pilot):
        app.screen.query_one("#invite", Input).value = invite_line(SCHED, "tok")
        await pilot.press("enter")
        await settle(pilot)
        assert isinstance(app.screen, PoolScreen)

    drive(app, steps)
    assert ("GET", "/pools/self", "Bearer tok") in calls
    saved = profile.load()
    assert saved["pool_token"] == "tok"
    assert saved["scheduler"] == SCHED
    assert app.scheduler == SCHED


def test_pool_screen_shows_invite_and_creates_with_admin_token(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED, admin_token="sat")

    async def steps(pilot):
        screen = app.screen
        assert screen.query_one("#pool-invite", Input).value == invite_line(
            SCHED, "tok"
        )
        screen.query_one("#new-pool", Input).focus()
        screen.query_one("#new-pool", Input).value = "lab2"
        await pilot.press("enter")
        await settle(pilot)
        result = str(screen.query_one("#create-result").render())
        assert "newjt" in result and "pat" in result

    drive(app, steps)
    # pool.create uses the server admin token, never the join token.
    assert ("POST", "/pools", "Bearer sat") in calls


def test_nodes_screen_fetches_with_saved_token(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await pilot.press("ctrl+n")
        await settle(pilot)
        screen = app.screen
        assert isinstance(screen, NodesScreen)
        assert screen.query_one("#nodes").row_count == 1
        assert screen.query_one("#jobs").row_count == 1

    drive(app, steps)
    assert ("GET", "/nodes", "Bearer tok") in calls
    assert ("GET", "/jobs", "Bearer tok") in calls


def test_logout_deletes_profile_and_shows_login(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await pilot.press("ctrl+l")
        await pilot.pause()
        assert isinstance(app.screen, LoginScreen)

    drive(app, steps)
    assert not cfg.exists()