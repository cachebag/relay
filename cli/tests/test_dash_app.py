import asyncio
import json

import httpx
import pytest
from textual.widgets import Input

from relay import profile
from relay.dash import DashApp, invite_line, parse_invite
from relay.dash.modals import JoinModal, NewPoolModal

_RealClient = httpx.Client

SCHED = "http://test:8080"
POOL = {"id": "p1", "name": "lab", "created_at": "2026-01-01T00:00:00Z"}
SAVED = {
    "scheduler": SCHED,
    "pool_id": "p1",
    "pool_name": "lab",
    "pool_token": "tok",
    "name": "akrm",
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
    "status": "assigned",
    "node_id": "n1",
    "attempts": 1,
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
        auth = req.headers.get("Authorization", "")
        if req.url.path == "/pools/self":
            if auth == "Bearer bad":
                return httpx.Response(403)
            pool = (
                POOL if auth == "Bearer tok" else {**POOL, "id": "p2", "name": "lab2"}
            )
            return httpx.Response(200, json=pool)
        if req.url.path == "/pools" and req.method == "POST":
            if auth != "Bearer sat":
                return httpx.Response(401)
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
        async with app.run_test(size=(120, 40)) as pilot:
            await steps(pilot)

    asyncio.run(run())


def text(widget) -> str:
    return str(widget.render())


def main(app):
    """The dashboard screen itself, underneath any open modal."""
    return app.screen_stack[0]


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
    # The placeholder relay pool create prints on localhost is not an address.
    placeholder = invite_line("http://<your address>:8080", "jt")
    assert parse_invite(placeholder, SCHED) == (SCHED, "jt")
    with pytest.raises(ValueError):
        parse_invite("relay-agent --scheduler http://x", SCHED)


def test_asks_to_join_without_saved_login(cfg, calls):
    app = DashApp(SCHED)

    async def steps(pilot):
        assert isinstance(app.screen, JoinModal)

    drive(app, steps)
    assert calls == []


def test_shows_pool_and_tables_with_saved_login(cfg, calls):
    profile.save(SAVED)
    app = DashApp("http://ignored")

    async def steps(pilot):
        await settle(pilot)
        assert not isinstance(app.screen, JoinModal)
        assert "lab" in text(app.query_one("#where"))
        assert app.invite_text == invite_line(SCHED, "tok")
        nodes, jobs = app.query_one("#nodes"), app.query_one("#jobs")
        assert nodes.row_count == 1 and jobs.row_count == 1
        # The jobs table shows the node's hostname, not its ID.
        assert "box" in str(jobs.get_row_at(0)[2])

    drive(app, steps)
    assert ("GET", "/nodes", "Bearer tok") in calls
    assert ("GET", "/jobs", "Bearer tok") in calls


def test_join_modal_logs_in_with_pasted_token(cfg, calls):
    app = DashApp("http://ignored")

    async def steps(pilot):
        app.screen.query_one("#invite", Input).value = invite_line(SCHED, "tok")
        await pilot.press("enter")
        await settle(pilot)
        assert not isinstance(app.screen, JoinModal)
        assert main(app).query_one("#nodes").row_count == 1

    drive(app, steps)
    assert ("GET", "/pools/self", "Bearer tok") in calls
    assert profile.load()["pool_token"] == "tok"
    assert app.scheduler == SCHED


def test_join_modal_shows_error_for_bad_token(cfg, calls):
    app = DashApp(SCHED)

    async def steps(pilot):
        app.screen.query_one("#invite", Input).value = "bad"
        await pilot.press("enter")
        await settle(pilot)
        assert isinstance(app.screen, JoinModal)
        assert "isn't valid" in text(app.screen.query_one("#join-error"))

    drive(app, steps)
    assert not cfg.exists()


def test_new_pool_from_join_modal_creates_and_logs_in(cfg, calls):
    app = DashApp(SCHED, admin_token="sat")

    async def steps(pilot):
        await pilot.press("ctrl+n")
        await pilot.pause()
        assert isinstance(app.screen, NewPoolModal)
        app.screen.query_one("#pool-name", Input).value = "lab2"
        await pilot.press("enter")
        await settle(pilot)
        result = text(app.screen.query_one("#create-result"))
        assert "newjt" in result and "pat" in result
        await pilot.press("enter")
        await settle(pilot)
        assert not isinstance(app.screen, NewPoolModal)

    drive(app, steps)
    # pool.create uses the server admin token, never the join token.
    assert ("POST", "/pools", "Bearer sat") in calls
    assert profile.load()["pool_token"] == "newjt"


def test_new_pool_from_dashboard_with_typed_admin_token(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("n")
        await pilot.pause()
        assert isinstance(app.screen, NewPoolModal)
        app.screen.query_one("#admin-token", Input).value = "wrong"
        app.screen.query_one("#pool-name", Input).focus()
        app.screen.query_one("#pool-name", Input).value = "lab2"
        await pilot.press("enter")
        await settle(pilot)
        assert "rejected" in text(app.screen.query_one("#create-error"))
        await pilot.press("escape")
        await pilot.pause()
        assert not isinstance(app.screen, NewPoolModal)

    drive(app, steps)
    assert ("POST", "/pools", "Bearer wrong") in calls
    assert profile.load()["pool_token"] == "tok"


def test_copy_invite(cfg, calls, monkeypatch):
    profile.save(SAVED)
    app = DashApp(SCHED)
    copied = []
    monkeypatch.setattr(DashApp, "copy_to_clipboard", lambda self, t: copied.append(t))

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("c")

    drive(app, steps)
    assert copied == [invite_line(SCHED, "tok")]


def test_logout_deletes_profile_and_asks_to_join(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("l")
        await pilot.pause()
        assert isinstance(app.screen, JoinModal)
        assert app.invite_text == ""

    drive(app, steps)
    assert not cfg.exists()
