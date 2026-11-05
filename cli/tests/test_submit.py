import argparse
import json
from types import SimpleNamespace

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
    monkeypatch.setattr(profile, "load", dict)

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


# Helper for testing CLI requests without a real scheduler.
def _mock_cli(monkeypatch, handler):
    """Use a saved login and route every CLI HTTP request to MockTransport."""
    monkeypatch.setattr(
        profile,
        "load",
        lambda: {"pool_token": "test-token", "name": "alice"},
    )
    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)


# Creates the arguments normally passed by relay submit --wait.
def _wait_args(wait_timeout=None, timeout=None):
    """Build arguments for relay submit --wait."""
    return argparse.Namespace(
        scheduler="http://scheduler",
        command=["echo", "hello"],
        wait=True,
        timeout=timeout,
        wait_timeout=wait_timeout,
    )


def test_submit_wait_prints_each_state_once(monkeypatch, capsys):
    """A successful job reports changes once, prints output, and exits 0."""
    states = iter(["queued", "running", "running", "done"])
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append((request.method, request.url.path))

        if request.method == "POST" and request.url.path == "/jobs":
            return httpx.Response(
                201,
                json={"id": "job123", "status": "queued"},
            )

        if request.url.path == "/jobs/job123":
            state = next(states)
            return httpx.Response(
                200,
                json={
                    "id": "job123",
                    "status": state,
                    "node_id": "rafael" if state == "running" else None,
                    "attempts": 1 if state == "running" else 0,
                },
            )

        if request.url.path == "/jobs/job123/output":
            return httpx.Response(
                200,
                json={
                    "stdout": "machine-name\n",
                    "stderr": "",
                    "exit_code": 0,
                },
            )

        raise AssertionError(f"Unexpected request: {request.method} {request.url}")

    _mock_cli(monkeypatch, handler)

    # Skip the real polling delay so the test finishes immediately.
    monkeypatch.setattr(submit_command.time, "sleep", lambda _: None)

    with pytest.raises(SystemExit) as exc:
        submit_command.run(_wait_args())

    captured = capsys.readouterr()

    # Verify each state prints once and the job output appears afterward.
    assert exc.value.code == 0
    assert captured.out.splitlines() == [
        "job123  queued",
        "running on rafael",
        "done",
        "machine-name",
    ]
    assert captured.err == ""

    # Verify four status requests and only one output request.
    assert requests.count(("GET", "/jobs/job123")) == 4
    assert requests.count(("GET", "/jobs/job123/output")) == 1


def test_submit_wait_propagates_failed_exit_code(monkeypatch, capsys):
    """A failed job prints output and exits with its remote exit code."""

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/jobs":
            return httpx.Response(
                201,
                json={"id": "job123", "status": "queued"},
            )

        if request.url.path == "/jobs/job123":
            return httpx.Response(200, json={"status": "failed"})

        if request.url.path == "/jobs/job123/output":
            return httpx.Response(
                200,
                json={
                    "stdout": "",
                    "stderr": "command failed\n",
                    "exit_code": 3,
                },
            )

        raise AssertionError(f"Unexpected request: {request.url}")

    _mock_cli(monkeypatch, handler)

    with pytest.raises(SystemExit) as exc:
        submit_command.run(_wait_args())

    captured = capsys.readouterr()

    # Failed jobs must return their actual exit code, not always 1.
    assert exc.value.code == 3
    assert captured.out.splitlines() == ["job123  queued", "failed"]
    assert captured.err == "command failed\n"


def test_submit_wait_shows_requeued_attempt(monkeypatch, capsys):
    """A retry is reported, but unchanged states are not duplicated."""
    states = iter(
        [
            {"status": "running", "node_id": "rafael", "attempts": 1},
            {"status": "queued", "attempts": 1},
            {"status": "queued", "attempts": 1},
            {"status": "running", "node_id": "rafael", "attempts": 2},
            {"status": "done", "attempts": 2},
        ]
    )

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/jobs":
            return httpx.Response(
                201,
                json={"id": "job123", "status": "queued"},
            )

        if request.url.path == "/jobs/job123":
            return httpx.Response(200, json=next(states))

        if request.url.path == "/jobs/job123/output":
            return httpx.Response(
                200,
                json={"stdout": "ok\n", "stderr": "", "exit_code": 0},
            )

        raise AssertionError(f"Unexpected request: {request.url}")

    _mock_cli(monkeypatch, handler)
    monkeypatch.setattr(submit_command.time, "sleep", lambda _: None)

    with pytest.raises(SystemExit) as exc:
        submit_command.run(_wait_args())

    # Verify the retry appears without repeating the unchanged queued state.
    assert exc.value.code == 0
    assert capsys.readouterr().out.splitlines() == [
        "job123  queued",
        "running on rafael",
        "requeued, attempt 2",
        "running on rafael, attempt 2",
        "done, attempt 2",
        "ok",
    ]


def test_submit_wait_timeout_exits_124(monkeypatch, capsys):
    """A never-ending job times out without being cancelled remotely."""
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append((request.method, request.url.path))

        if request.method == "POST" and request.url.path == "/jobs":
            return httpx.Response(
                201,
                json={"id": "job123", "status": "queued"},
            )

        if request.url.path == "/jobs/job123":
            return httpx.Response(200, json={"status": "queued"})

        raise AssertionError(f"Unexpected request: {request.method} {request.url}")

    _mock_cli(monkeypatch, handler)

    # Simulate one second passing instead of actually waiting.
    elapsed = [0.0]

    def fake_sleep(seconds):
        elapsed[0] += seconds

    monkeypatch.setattr(
        submit_command,
        "time",
        SimpleNamespace(
            monotonic=lambda: elapsed[0],
            sleep=fake_sleep,
        ),
    )

    with pytest.raises(SystemExit) as exc:
        submit_command.run(_wait_args(wait_timeout=1))

    captured = capsys.readouterr()

    # Timeout must exit 124 and explain how to check the job later.
    assert exc.value.code == 124
    assert captured.out == "job123  queued\n"
    assert captured.err == ("still running, check later with relay status job123\n")

    # The client must not request output or cancel the remote job.
    assert requests == [
        ("POST", "/jobs"),
        ("GET", "/jobs/job123"),
    ]


def test_submit_wait_keyboard_interrupt_exits_130(monkeypatch, capsys):
    """Ctrl+C during polling stops waiting without cancelling the job."""
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append((request.method, request.url.path))

        if request.method == "POST" and request.url.path == "/jobs":
            return httpx.Response(
                201,
                json={"id": "job123", "status": "queued"},
            )

        # Simulate Ctrl+C while fetching the next job status.
        if request.url.path == "/jobs/job123":
            raise KeyboardInterrupt

        raise AssertionError(f"Unexpected request: {request.url}")

    _mock_cli(monkeypatch, handler)

    with pytest.raises(SystemExit) as exc:
        submit_command.run(_wait_args())

    captured = capsys.readouterr()

    # Ctrl+C must exit 130 and leave the submitted job alone.
    assert exc.value.code == 130
    assert captured.out == "job123  queued\n"
    assert captured.err == ("still running, check later with relay status job123\n")
    assert requests == [
        ("POST", "/jobs"),
        ("GET", "/jobs/job123"),
    ]


def test_submit_wait_timeout_requires_wait(monkeypatch, capsys):
    """The local wait limit requires --wait."""
    monkeypatch.setattr(
        profile,
        "load",
        lambda: pytest.fail("should not submit"),
    )

    args = _wait_args(wait_timeout=1)
    args.wait = False

    with pytest.raises(SystemExit) as exc:
        submit_command.run(args)

    assert exc.value.code == 2
    assert "--wait-timeout requires --wait" in capsys.readouterr().err


@pytest.mark.parametrize(
    "invalid_timeout",
    [0, -1, float("nan"), float("inf")],
)
def test_submit_rejects_invalid_wait_timeout(monkeypatch, capsys, invalid_timeout):
    """Invalid local waiting limits are rejected before submission."""
    monkeypatch.setattr(
        profile,
        "load",
        lambda: pytest.fail("should not submit"),
    )

    with pytest.raises(SystemExit) as exc:
        submit_command.run(_wait_args(wait_timeout=invalid_timeout))

    assert exc.value.code == 2
    assert "--wait-timeout must be a positive number" in capsys.readouterr().err


def test_submit_register_accepts_wait_and_timeout():
    """The CLI accepts separate execution and waiting time limits."""
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers()
    submit_command.register(sub)

    args = parser.parse_args(
        [
            "submit",
            "--wait",
            "--timeout",
            "10m",
            "--wait-timeout",
            "1.5",
            "--",
            "uname",
            "-n",
        ]
    )

    assert args.wait is True
    assert args.timeout == 600
    assert args.wait_timeout == 1.5
    assert args.command == ["uname", "-n"]


def test_submit_register_accepts_os(monkeypatch):
    """The CLI parser accepts the --os option for specifying the required OS."""
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers()
    submit_command.register(sub)

    args = parser.parse_args(["submit", "--os", "linux", "--", "uname", "-n"])

    assert args.os == "linux"
    assert args.command == ["uname", "-n"]


def test_submit_register_rejects_invalid_os(monkeypatch, capsys):
    """The CLI parser rejects invalid values for the --os option."""
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers()
    submit_command.register(sub)

    with pytest.raises(SystemExit) as exc:
        parser.parse_args(["submit", "--os", "invalid-os", "--", "uname", "-n"])

    assert exc.value.code == 2
    assert "invalid choice" in capsys.readouterr().err


def test_submit_register_accepts_no_os(monkeypatch):
    """The CLI parser correctly handles the absence of the --os option."""
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers()
    submit_command.register(sub)

    args = parser.parse_args(["submit", "--", "uname", "-n"])

    assert args.os is None
    assert args.command == ["uname", "-n"]


def test_submit_posts_os_requirement():
    """--os is sent as requires.os; without it the body has no requires key."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert json.loads(request.content) == {
            "command": ["uname", "-a"],
            "submitter": "alice",
            "requires": {"os": "linux"},
        }
        return httpx.Response(201, json={"id": "abc123", "status": "queued"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    submit(
        "http://scheduler", ["uname", "-a"], "tok", "alice", client=client, os="linux"
    )


def test_run_submit_sends_os_from_flag(monkeypatch, capsys):
    """relay submit --os linux puts the requirement in the request."""
    monkeypatch.setattr(
        profile,
        "load",
        lambda: {"scheduler": "http://scheduler", "pool_token": "tok", "name": "alice"},
    )

    def handler(request: httpx.Request) -> httpx.Response:
        assert json.loads(request.content)["requires"] == {"os": "linux"}
        return httpx.Response(201, json={"id": "job123", "status": "queued"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)

    submit_command.run(
        argparse.Namespace(
            scheduler="http://scheduler", command=["uname", "-a"], os="linux"
        )
    )
    assert "job123" in capsys.readouterr().out
