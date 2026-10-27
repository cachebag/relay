import argparse
from datetime import UTC, datetime

import httpx
from relay.commands import jobs


def test_fetch_sends_join_token() -> None:
    """jobs.fetch sends the join token; the scheduler picks the pool from it."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/jobs"
        assert request.headers["Authorization"] == "Bearer tok-123"
        assert "pool" not in request.url.params

        return httpx.Response(200, json=[])

    client = httpx.Client(transport=httpx.MockTransport(handler))

    result = jobs.fetch(
        "http://test",
        "tok-123",
        client=client,
    )

    assert result == []


def test_jobs_displays_submitter(monkeypatch, capsys) -> None:
    """The jobs table includes the user who submitted each job."""
    now = datetime.now(UTC).isoformat()

    test_jobs = [
        {
            "id": "job-1",
            "status": "done",
            "node_id": "node-1",
            "attempts": 1,
            "created_at": now,
            "submitter": "alice",
            "command": ["echo", "hello"],
        },
        {
            "id": "job-2",
            "status": "queued",
            "node_id": "",
            "attempts": 0,
            "created_at": now,
            "submitter": "bob",
            "command": ["sleep", "5"],
        },
    ]

    monkeypatch.setattr(jobs.profile, "load", lambda: {"pool_token": "tok"})
    monkeypatch.setattr(
        jobs,
        "fetch",
        lambda scheduler, token: test_jobs,
    )

    args = argparse.Namespace(scheduler="http://scheduler")

    jobs.run(args)

    output = capsys.readouterr().out

    assert "SUBMITTER" in output
    assert "alice" in output
    assert "bob" in output
    assert "job-1" in output
    assert "job-2" in output
    assert "echo hello" in output
    assert "sleep 5" in output


def test_jobs_missing_submitter(monkeypatch, capsys) -> None:
    """Older jobs without submitter information display a dash."""
    now = datetime.now(UTC).isoformat()

    test_jobs = [
        {
            "id": "old-job",
            "status": "done",
            "node_id": "",
            "attempts": 1,
            "created_at": now,
            "command": ["echo", "old"],
        },
    ]

    monkeypatch.setattr(jobs.profile, "load", lambda: {"pool_token": "tok"})
    monkeypatch.setattr(
        jobs,
        "fetch",
        lambda scheduler, token: test_jobs,
    )

    args = argparse.Namespace(scheduler="http://scheduler")

    jobs.run(args)

    output = capsys.readouterr().out

    assert "SUBMITTER" in output
    assert "old-job" in output
    assert "-" in output
    assert "echo old" in output


def test_jobs_requires_os(monkeypatch, capsys) -> None:
    """Jobs that require a specific OS display it in the REQUIRES column."""
    now = datetime.now(UTC).isoformat()

    test_jobs = [
        {
            "id": "job-1",
            "status": "done",
            "node_id": "node-1",
            "attempts": 1,
            "created_at": now,
            "requires": {"os": "linux"},
            "command": ["echo", "hello"],
        },
    ]

    monkeypatch.setattr(jobs.profile, "load", lambda: {"pool_token": "tok"})
    monkeypatch.setattr(
        jobs,
        "fetch",
        lambda scheduler, token: test_jobs,
    )

    args = argparse.Namespace(scheduler="http://scheduler")

    jobs.run(args)

    output = capsys.readouterr().out

    assert "REQUIRES" in output
    assert "linux" in output
    assert "job-1" in output
    assert "echo hello" in output
