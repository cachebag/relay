import asyncio
import json

import httpx
import pytest
from textual.widgets import Input

from relay import profile
from relay.dash import DashApp, invite_line, parse_invite
from relay.dash.admin import AdminModal
from relay.dash.credits import CreditsModal
from relay.dash.job import JobModal
from relay.dash.modals import JoinModal, NewPoolModal, PoolPicker

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
    "status": "done",
    "node_id": "n1",
    "attempts": 1,
    "submitter": "akrm",
    "created_at": "2026-01-01T00:00:00+00:00",
    "started_at": "2026-01-01T00:00:05+00:00",
    "finished_at": "2026-01-01T00:00:09+00:00",
    "command": ["uname", "-n"],
}
OLDER_JOB = {**JOB, "id": "j0", "status": "failed", "command": ["false"]}
OUTPUT = {"stdout": "box\n", "stderr": "", "exit_code": 0}
CREDITS = [{"account": "akrm", "balance": 42}, {"account": "omar", "balance": 7}]


class Calls(list):
    """Every request as (method, path, auth), plus JSON bodies by (method, path)."""

    bodies: dict


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    return tmp_path / "relay" / profile.FILENAME


@pytest.fixture
def calls(monkeypatch):
    """Route every httpx.Client through a MockTransport and record requests."""
    seen = Calls()
    seen.bodies = {}

    def handler(req: httpx.Request) -> httpx.Response:
        seen.append((req.method, req.url.path, req.headers.get("Authorization")))
        auth = req.headers.get("Authorization", "")
        if req.content:
            seen.bodies[(req.method, req.url.path)] = json.loads(req.content)
        if req.url.path == "/jobs" and req.method == "POST":
            return httpx.Response(201, json={**JOB, "id": "j2", "status": "queued"})
        if req.url.path == "/jobs/j1":
            return httpx.Response(200, json=JOB)
        if req.url.path == "/jobs/j1/output":
            return httpx.Response(200, json=OUTPUT)
        if req.url.path == "/pools/p1/credits":
            return httpx.Response(200, json=CREDITS)
        if req.url.path == "/pools/p1/token" and req.method == "POST":
            if auth != "Bearer pat":
                return httpx.Response(401)
            return httpx.Response(200, json={"token": "newjt2"})
        if req.url.path == "/pools/p1/nodes/n1" and req.method == "DELETE":
            if auth != "Bearer pat":
                return httpx.Response(401)
            return httpx.Response(204)
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
            return httpx.Response(200, json=[JOB, OLDER_JOB])
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
        assert nodes.row_count == 1 and jobs.row_count == 2
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


def test_submit_box_posts_the_command(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)
    notes = []
    app.notify = lambda msg, **kw: notes.append(msg)

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("s")
        box = app.query_one("#submit", Input)
        assert box.has_focus
        box.value = "echo 'hello world'"
        await pilot.press("enter")
        await settle(pilot)
        assert box.value == ""

    drive(app, steps)
    assert ("POST", "/jobs", "Bearer tok") in calls
    assert calls.bodies[("POST", "/jobs")] == {
        "command": ["echo", "hello world"],
        "submitter": "akrm",
    }
    assert notes == ["submitted j2"]


def test_enter_on_a_job_shows_its_output(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        assert main(app).query_one("#jobs").has_focus
        await pilot.press("enter")
        await settle(pilot)
        assert isinstance(app.screen, JobModal)
        summary = text(app.screen.query_one("#job-summary"))
        assert "uname -n" in summary and "box" in summary and "ran" in summary
        assert "exit code  0" in summary
        assert text(app.screen.query_one("#job-stdout")).strip() == "box"
        await pilot.press("escape")
        await pilot.pause()
        assert not isinstance(app.screen, JobModal)

    drive(app, steps)
    assert ("GET", "/jobs/j1", "Bearer tok") in calls
    assert ("GET", "/jobs/j1/output", "Bearer tok") in calls


def test_credits_modal_marks_me(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("k")
        await settle(pilot)
        assert isinstance(app.screen, CreditsModal)
        table = app.screen.query_one("#credits")
        assert table.row_count == 2
        assert [str(c) for c in table.get_row_at(0)] == ["*", "akrm", "42"]

    drive(app, steps)
    assert ("GET", "/pools/p1/credits", "Bearer tok") in calls


async def confirm(pilot, app, admin_token, name):
    app.screen.query_one("#admin-token", Input).value = admin_token
    box = app.screen.query_one("#confirm-name", Input)
    box.focus()
    box.value = name
    await pilot.press("enter")
    await settle(pilot)


def test_rotate_token_needs_pool_name_and_admin_token(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("t")
        await pilot.pause()
        assert isinstance(app.screen, AdminModal)
        await confirm(pilot, app, "", "lab")
        assert "token is needed" in text(app.screen.query_one("#admin-error"))
        await confirm(pilot, app, "pat", "nope")
        assert "isn't this pool's name" in text(app.screen.query_one("#admin-error"))
        assert ("POST", "/pools/p1/token", "Bearer pat") not in calls
        await confirm(pilot, app, "pat", "lab")
        assert "newjt2" in text(app.screen.query_one("#admin-result"))
        await pilot.press("escape")
        await settle(pilot)
        assert not isinstance(app.screen, AdminModal)
        assert app.invite_text == invite_line(SCHED, "newjt2")

    drive(app, steps)
    assert ("POST", "/pools/p1/token", "Bearer pat") in calls
    assert profile.load()["pool_token"] == "newjt2"
    assert app.pool_admin_token == "pat"


def test_remove_node_deletes_the_highlighted_machine(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("x")
        await pilot.pause()
        assert isinstance(app.screen, AdminModal)
        assert "box" in text(app.screen.query_one(".title"))
        await confirm(pilot, app, "wrong", "lab")
        assert "rejected" in text(app.screen.query_one("#admin-error"))
        await confirm(pilot, app, "pat", "lab")
        assert "removed box" in text(app.screen.query_one("#admin-result"))
        await pilot.press("escape")
        await settle(pilot)

    drive(app, steps)
    assert ("DELETE", "/pools/p1/nodes/n1", "Bearer wrong") in calls
    assert ("DELETE", "/pools/p1/nodes/n1", "Bearer pat") in calls


def test_redraw_keeps_the_cursor_where_it_was(cfg, calls):
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        jobs = main(app).query_one("#jobs")
        await pilot.press("down")
        assert jobs.cursor_row == 1
        # The one-second tick and the two-second fetch both go through redraw.
        app.redraw()
        await pilot.pause()
        assert jobs.cursor_row == 1
        # Rows changing underneath still keeps the highlighted job.
        app.job_rows = [OLDER_JOB, JOB]
        app.redraw()
        await pilot.pause()
        assert jobs.cursor_row == 0
        assert jobs.highlighted_key() == "j0"

    drive(app, steps)


def test_ctrl_c_copies_the_highlighted_id_or_the_output(cfg, calls, monkeypatch):
    profile.save(SAVED)
    app = DashApp(SCHED)
    copied = []
    monkeypatch.setattr(DashApp, "copy_to_clipboard", lambda self, t: copied.append(t))

    async def steps(pilot):
        await settle(pilot)
        await pilot.press("down", "ctrl+c")
        assert copied == ["j0"]
        await pilot.press("up", "enter")
        await settle(pilot)
        assert isinstance(app.screen, JobModal)
        await pilot.press("ctrl+c")
        assert copied == ["j0", "box\n"]

    drive(app, steps)


HOME = {**SAVED, "pool_id": "p2", "pool_name": "home", "pool_token": "tok2"}


def test_pool_picker_switches_and_logout_falls_back(cfg, calls):
    profile.save(HOME)
    profile.save(SAVED)
    app = DashApp(SCHED)

    async def steps(pilot):
        await settle(pilot)
        assert app.invite_text == invite_line(SCHED, "tok")
        await pilot.press("p")
        await pilot.pause()
        assert isinstance(app.screen, PoolPicker)
        table = app.screen.query_one("#pools")
        assert table.row_count == 2
        await pilot.press("up", "enter")
        await settle(pilot)
        assert not isinstance(app.screen, PoolPicker)
        assert app.prof["pool_name"] == "home"
        assert app.invite_text == invite_line(SCHED, "tok2")
        # Logging out of home lands in lab rather than the join box.
        await pilot.press("l")
        await settle(pilot)
        assert not isinstance(app.screen, JoinModal)
        assert app.prof["pool_name"] == "lab"

    drive(app, steps)
    assert ("GET", "/nodes", "Bearer tok2") in calls
    assert profile.logins() == [SAVED]
