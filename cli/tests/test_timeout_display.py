"""Time limits and remaining time in CLI status and dashboard job details."""

import argparse
import asyncio
from datetime import UTC, datetime, timedelta

from textual.app import App, ComposeResult
from textual.widgets import Static

from relay.commands import status
from relay.dash.job import JobModal


def running_job(started_at: str, timeout_seconds: int = 600) -> dict:
    return {
        "id": "j1",
        "command": ["sleep", "600"],
        "status": "running",
        "node_id": "n1",
        "attempts": 1,
        "timeout_seconds": timeout_seconds,
        "started_at": started_at,
    }


def test_time_limit_defaults_and_formats():
    assert status.time_limit({}) == "1h"
    assert status.time_limit({"timeout_seconds": 0}) == "1h"
    assert status.time_limit({"timeout_seconds": 5}) == "5s"
    assert status.time_limit({"timeout_seconds": 90}) == "1m30s"
    assert status.time_limit({"timeout_seconds": 600}) == "10m"
    assert status.time_limit({"timeout_seconds": 5400}) == "1h30m"
    assert status.time_limit({"timeout_seconds": 86400}) == "24h"


def test_time_left_counts_down_and_stops_at_zero():
    now = datetime(2026, 11, 1, 12, 0, tzinfo=UTC)

    job = running_job(
        (now - timedelta(seconds=45)).isoformat()
    )
    assert status.time_left(job, now) == "9m15s"

    job["started_at"] = (
        now - timedelta(seconds=700)
    ).isoformat()
    assert status.time_left(job, now) == "0s"

    job["started_at"] = (
        now + timedelta(seconds=10)
    ).isoformat()
    assert status.time_left(job, now) == "10m"


def test_time_left_only_for_running_jobs():
    job = running_job("2026-11-01T12:00:00Z")

    assert status.time_left(
        {**job, "status": "queued"}
    ) is None

    assert status.time_left(
        {**job, "status": "done"}
    ) is None

    assert status.time_left(
        {**job, "started_at": None}
    ) is None


def test_status_prints_limit_and_time_left(monkeypatch, capsys):
    started = datetime.now(UTC) - timedelta(seconds=30)
    job = running_job(started.isoformat())

    monkeypatch.setattr(
        status.profile,
        "require_login",
        lambda: {"pool_token": "tok"},
    )
    monkeypatch.setattr(
        status,
        "fetch",
        lambda *args: job,
    )

    status.run(
        argparse.Namespace(
            scheduler="http://scheduler",
            job_id="j1",
        )
    )

    output = capsys.readouterr().out

    assert "limit=10m" in output
    assert "left=9m" in output
    assert "running for" in output


def test_status_prints_limit_for_queued_job(monkeypatch, capsys):
    job = {
        **running_job("2026-11-01T12:00:00Z"),
        "status": "queued",
        "started_at": None,
        "timeout_seconds": None,
    }

    monkeypatch.setattr(
        status.profile,
        "require_login",
        lambda: {"pool_token": "tok"},
    )
    monkeypatch.setattr(
        status,
        "fetch",
        lambda *args: job,
    )

    status.run(
        argparse.Namespace(
            scheduler="http://scheduler",
            job_id="j1",
        )
    )

    output = capsys.readouterr().out

    assert "limit=1h" in output
    assert "left=" not in output


class ModalHarness(App):
    def compose(self) -> ComposeResult:
        yield Static("ready")


def test_dashboard_job_modal_displays_limit_and_time_left(monkeypatch):
    # Keep the modal local: don't start its normal scheduler polling worker.
    monkeypatch.setattr(
        JobModal,
        "load",
        lambda self: None,
    )

    async def check():
        app = ModalHarness()

        async with app.run_test() as pilot:
            modal = JobModal("j1", {"n1": "worker"})
            await app.push_screen(modal)
            await pilot.pause()

            started = datetime.now(UTC) - timedelta(seconds=30)

            modal.show(
                running_job(started.isoformat()),
                409,
                None,
            )

            summary = str(
                modal.query_one("#job-summary", Static).render()
            )

            assert "time limit 10m" in summary
            assert "time left" in summary
            assert "9m" in summary

            modal.show(
                {
                    **running_job(started.isoformat()),
                    "status": "done",
                },
                200,
                {},
            )

            summary = str(
                modal.query_one("#job-summary", Static).render()
            )

            assert "time limit 10m" in summary
            assert "time left" not in summary

    asyncio.run(check())
