import argparse
import json

import httpx
import pytest

from relay import profile
from relay.commands import submit as submit_command
from relay.commands.submit import submit


@pytest.mark.parametrize(
    ("value", "seconds"),
    [
        ("5", 5),
        ("600", 600),
        ("5s", 5),
        ("10m", 600),
        ("1h30m", 5400),
        ("1.5h", 5400),
        ("24h", 86400),
    ],
)
def test_parse_job_timeout(value, seconds):
    assert submit_command.parse_job_timeout(value) == seconds


@pytest.mark.parametrize(
    "value",
    [
        "0",
        "0s",
        "-1",
        "-5m",
        "86401",
        "24h1s",
        "abc",
        "1.5",
        "5x",
        "1hgarbage",
        "1500ms",
        "1ns",
        "5us",
        "5µs",
        "5μs",
    ],
)
def test_parse_job_timeout_rejects_invalid_values(value):
    with pytest.raises(argparse.ArgumentTypeError):
        submit_command.parse_job_timeout(value)


def test_submit_posts_job_timeout():
    """An explicit time limit is sent to POST /jobs alongside the OS."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert json.loads(request.content) == {
            "command": ["echo", "hi"],
            "submitter": "alice",
            "requires": {"os": "linux"},
            "timeout_seconds": 600,
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
        os="linux",
        timeout_seconds=600,
    )

    assert job["id"] == "abc123"


def test_submit_register_rejects_invalid_duration(capsys):
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers()
    submit_command.register(sub)

    with pytest.raises(SystemExit) as exc:
        parser.parse_args(["submit", "--timeout", "25h", "--", "echo", "hi"])

    assert exc.value.code == 2
    assert "24 hours" in capsys.readouterr().err


def test_run_submit_sends_timeout_without_wait(monkeypatch, capsys):
    """The job limit can be set without enabling local polling."""

    monkeypatch.setattr(
        profile,
        "load",
        lambda: {
            "pool_token": "test-token",
            "name": "alice",
        },
    )

    def handler(request: httpx.Request) -> httpx.Response:
        assert json.loads(request.content) == {
            "command": ["echo", "hello"],
            "submitter": "alice",
            "timeout_seconds": 5,
        }

        return httpx.Response(
            201,
            json={"id": "job123", "status": "queued"},
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)

    submit_command.run(
        argparse.Namespace(
            scheduler="http://scheduler",
            command=["echo", "hello"],
            timeout=5,
            wait=False,
        )
    )

    assert "job123  queued" in capsys.readouterr().out
