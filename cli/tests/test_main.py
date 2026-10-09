import sys

import httpx
import pytest

import relay.__main__ as relay_main


def register_test_command(subparsers, func):
    parser = subparsers.add_parser("test-command")
    parser.set_defaults(func=func)


def test_scheduler_defaults_to_env(monkeypatch):
    monkeypatch.setenv("RELAY_SCHEDULER", "http://x:1")

    actual = {}

    def command(args):
        actual["scheduler"] = args.scheduler

    monkeypatch.setattr(
        relay_main.nodes,
        "register",
        lambda subparsers: register_test_command(subparsers, command),
    )
    monkeypatch.setattr(sys, "argv", ["relay", "test-command"])

    relay_main.main()

    assert actual["scheduler"] == "http://x:1"


def test_scheduler_flag_overrides_env(monkeypatch):
    monkeypatch.setenv("RELAY_SCHEDULER", "http://x:1")

    actual = {}

    def command(args):
        actual["scheduler"] = args.scheduler

    monkeypatch.setattr(
        relay_main.nodes,
        "register",
        lambda subparsers: register_test_command(subparsers, command),
    )
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "relay",
            "--scheduler",
            "http://flag:2",
            "test-command",
        ],
    )

    relay_main.main()

    assert actual["scheduler"] == "http://flag:2"


def test_connection_error_is_friendly(monkeypatch, capsys):
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection failed", request=request)

    transport = httpx.MockTransport(handler)
    client = httpx.Client(transport=transport)

    def command(args):
        client.get(args.scheduler)

    monkeypatch.setattr(
        relay_main.nodes,
        "register",
        lambda subparsers: register_test_command(subparsers, command),
    )
    monkeypatch.setattr(
        sys,
        "argv",
        [
            "relay",
            "--scheduler",
            "http://unreachable",
            "test-command",
        ],
    )

    with pytest.raises(SystemExit) as exc:
        relay_main.main()

    assert exc.value.code == 1

    captured = capsys.readouterr()
    assert (
        captured.err
        == "relay: can't reach scheduler at http://unreachable\n"
    )
    assert "Traceback" not in captured.err

    def test_empty_scheduler_env_uses_default(monkeypatch):
        monkeypatch.setenv("RELAY_SCHEDULER", "")

        actual = {}

        def command(args):
            actual["scheduler"] = args.scheduler

        monkeypatch.setattr(
            relay_main.nodes,
            "register",
            lambda subparsers: register_test_command(subparsers, command),
        )
        monkeypatch.setattr(sys, "argv", ["relay", "test-command"])

        relay_main.main()

        assert actual["scheduler"] == "http://localhost:8080"