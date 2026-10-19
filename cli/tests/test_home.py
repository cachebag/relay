import asyncio

import pytest

from relay.commands import home


@pytest.fixture(autouse=True)
def offline(monkeypatch):
    def boom(*_a, **_k):
        raise ValueError("no scheduler")

    monkeypatch.setattr(home, "fetch", boom)


def test_menu_and_status_render():
    app = home.HomeApp("http://localhost:0", "tok")

    async def drive():
        async with app.run_test() as pilot:
            await pilot.pause()
            labels = [str(b.label) for b in app.query(home.MenuItem)]
            assert labels == ["Dashboard", "Sign in", "Sign up", "Quit"]
            assert app.query_one("#menu-signin").disabled
            assert app.query_one("#menu-dash").has_focus
            assert "unreachable" in str(app.query_one("#pool-state").render())

    asyncio.run(drive())


def test_enter_on_dashboard_exits_with_choice():
    app = home.HomeApp("http://localhost:0", "tok")

    async def drive():
        async with app.run_test() as pilot:
            await pilot.press("enter")

    asyncio.run(drive())
    assert app.return_value == "dash"


def test_quit_exits_with_none():
    app = home.HomeApp("http://localhost:0", "tok")

    async def drive():
        async with app.run_test() as pilot:
            await pilot.press("down", "down", "down", "enter")

    asyncio.run(drive())
    assert app.return_value is None


def test_dashboard_escape_returns_home(monkeypatch):
    from relay.commands import dash

    monkeypatch.setattr(dash, "fetch", lambda *_a, **_k: [])
    app = dash.Dashboard("http://localhost:0", "tok")

    async def drive():
        async with app.run_test() as pilot:
            await pilot.press("escape")

    asyncio.run(drive())
    assert app.return_value == "home"


def test_run_loops_home_and_dash(monkeypatch):
    from types import SimpleNamespace

    from relay.commands import dash

    calls = []

    class FakeHome:
        results = iter(["dash", "dash", None])

        def __init__(self, *_a):
            pass

        def run(self):
            calls.append("home")
            return next(self.results)

    class FakeDash:
        results = iter(["home", None])

        def __init__(self, *_a):
            pass

        def run(self):
            calls.append("dash")
            return next(self.results)

    monkeypatch.setattr(home, "HomeApp", FakeHome)
    monkeypatch.setattr(dash, "Dashboard", FakeDash)
    monkeypatch.setattr(home.profile, "load", lambda: {"pool_token": "tok"})
    home.run(SimpleNamespace(scheduler="x"))
    assert calls == ["home", "dash", "home", "dash"]
