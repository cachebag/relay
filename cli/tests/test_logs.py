from types import SimpleNamespace

import httpx
import pytest
from relay.commands import logs


def test_run_prints_output_and_exits_with_job_exit_code(capsys) -> None:
    """logs.run should print both streams and exit with the job's exit code."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/jobs/job-123/output"

        return httpx.Response(
            200,
            json={
                "stdout": "normal output\n",
                "stderr": "error output\n",
                "exit_code": 7,
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))

    args = SimpleNamespace(
        scheduler="http://test",
        id="job-123",
    )

    with pytest.raises(SystemExit) as exc:
        logs.run(args, client=client)

    captured = capsys.readouterr()

    assert exc.value.code == 7
    assert captured.out == "normal output\n"
    assert captured.err == "error output\n"


def test_run_409_exits_one(capsys) -> None:
    """logs.run should exit 1 when the job has not finished."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(409)

    client = httpx.Client(transport=httpx.MockTransport(handler))

    args = SimpleNamespace(
        scheduler="http://test",
        id="job-123",
    )

    with pytest.raises(SystemExit) as exc:
        logs.run(args, client=client)

    captured = capsys.readouterr()

    assert exc.value.code == 1
    assert captured.out == ""
    assert captured.err == "job job-123 has not finished\n"


def test_run_404_exits_one(capsys) -> None:
    """logs.run should exit 1 when the requested job does not exist."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(404)

    client = httpx.Client(transport=httpx.MockTransport(handler))

    args = SimpleNamespace(
        scheduler="http://test",
        id="job-123",
    )

    with pytest.raises(SystemExit) as exc:
        logs.run(args, client=client)

    captured = capsys.readouterr()

    assert exc.value.code == 1
    assert captured.out == ""
    assert captured.err == "no such job\n"


def test_print_job_output_can_be_reused_without_exiting(capsys) -> None:
    """Other commands can print logs and receive the job exit code."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/jobs/job-123/output"
        return httpx.Response(
            200,
            json={
                "stdout": "normal output\n",
                "stderr": "error output\n",
                "exit_code": 3,
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))

    code = logs.print_job_output("http://test", "job-123", client=client)

    captured = capsys.readouterr()
    assert code == 3
    assert captured.out == "normal output\n"
    assert captured.err == "error output\n"
