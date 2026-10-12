
import argparse
import json

import httpx
import pytest

from relay import profile
from relay.commands import submit as submit_command
from relay.commands.submit import submit


def test_submit_posts_command():
    """Submission includes the pool token and submitter."""
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/jobs"
        assert request.headers["Authorization"] == "Bearer test-token"

        assert json.loads(request.content) == {
            "command": ["echo", "hi"],
            "submitter": "alice",
        }

        return httpx.Response(
            201,
            json={"id": "abc123", "status": "queued"},
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))

    job = submit(
        "http://scheduler",
        ["echo", "hi"],
        "test-token",
        "alice",
        client=client,
    )

    assert job["id"] == "abc123"
    assert job["status"] == "queued"


def test_submit_rejects_invalid_token():
    """A rejected pool token produces an HTTP status error."""
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer bad-token"
        return httpx.Response(403, text="invalid pool token")

    client = httpx.Client(transport=httpx.MockTransport(handler))

    with pytest.raises(httpx.HTTPStatusError) as exc:
        submit(
            "http://scheduler",
            ["echo", "hi"],
            "bad-token",
            "alice",
            client=client,
        )

    assert exc.value.response.status_code == 403


def test_run_submit_not_logged_in(monkeypatch, capsys):
    """Submitting without a saved profile exits with status 1."""
    monkeypatch.setattr(profile, "load", lambda: {})

    args = argparse.Namespace(
        scheduler="http://scheduler",
        command=["echo", "hi"],
    )

    with pytest.raises(SystemExit) as exc:
        submit_command.run(args)

    assert exc.value.code == 1
    assert "not logged in" in capsys.readouterr().err


def test_run_submit_incomplete_profile(monkeypatch, capsys):
    """A profile missing the token or name cannot submit."""
    profiles = [
        {"name": "alice"},
        {"pool_token": "test-token"},
        {"pool_token": "", "name": "alice"},
        {"pool_token": "test-token", "name": "   "},
    ]

    args = argparse.Namespace(
        scheduler="http://scheduler",
        command=["echo", "hi"],
    )

    for prof in profiles:
        monkeypatch.setattr(
            profile,
            "load",
            lambda p=prof: p,
        )

        with pytest.raises(SystemExit) as exc:
            submit_command.run(args)

        assert exc.value.code == 1
        assert "not logged in" in capsys.readouterr().err


def test_run_submit_uses_saved_profile(monkeypatch, capsys):
    """CLI submission reads both authentication values from the profile."""
    saved_profile = {
        "scheduler": "http://scheduler",
        "pool_id": "pool-123",
        "pool_token": "test-token",
        "name": "alice",
    }

    monkeypatch.setattr(profile, "load", lambda: saved_profile)

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/jobs"
        assert request.headers["Authorization"] == "Bearer test-token"

        assert json.loads(request.content) == {
            "command": ["echo", "hello"],
            "submitter": "alice",
        }

        return httpx.Response(
            201,
            json={"id": "job123", "status": "queued"},
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)

    args = argparse.Namespace(
        scheduler="http://scheduler",
        command=["echo", "hello"],
    )

    submit_command.run(args)

    output = capsys.readouterr().out
    assert "job123" in output
    assert "queued" in output


def test_run_submit_invalid_token(monkeypatch, capsys):
    """CLI displays a friendly error if the pool token is rejected."""
    monkeypatch.setattr(
        profile,
        "load",
        lambda: {
            "pool_token": "bad-token",
            "name": "alice",
        },
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(403, text="invalid pool token")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)

    args = argparse.Namespace(
        scheduler="http://scheduler",
        command=["echo", "hello"],
    )

    with pytest.raises(SystemExit) as exc:
        submit_command.run(args)

    assert exc.value.code == 1
    assert "invalid pool token" in capsys.readouterr().err
