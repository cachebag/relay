import argparse
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
    assert captured.err == "relay: can't reach scheduler at http://unreachable\n"
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


def test_resolve_survives_submit_argv():
    """`submit` has a positional named `command`, so the subparser's dest must
    not be `command` or resolve() sees the job's argv instead of the name."""
    args = argparse.Namespace(
        scheduler=None, subcommand="submit", command=["echo", "hi"]
    )
    relay_main.resolve(args, {}, {"scheduler": "http://saved:1"})
    assert args.scheduler == "http://saved:1"


@pytest.mark.parametrize(
    ("scheduler", "env", "prof", "server", "expected"),
    [
        # CLI flag with a trailing slash
        (
            "https://relay.example.ts.net/",
            {},
            {},
            {},
            "https://relay.example.ts.net",
        ),
        # Environment variable with multiple trailing slashes
        (
            None,
            {"RELAY_SCHEDULER": "https://relay.example.ts.net///"},
            {},
            {},
            "https://relay.example.ts.net",
        ),
        # Saved login with a trailing slash
        (
            None,
            {},
            {"scheduler": "https://relay.example.ts.net/"},
            {},
            "https://relay.example.ts.net",
        ),
        # Saved public URL with a trailing slash
        (
            None,
            {},
            {},
            {"public_url": "https://relay.example.ts.net/"},
            "https://relay.example.ts.net",
        ),
        # Server address with a trailing slash
        (
            None,
            {},
            {},
            {"address": "https://relay.example.ts.net/"},
            "https://relay.example.ts.net",
        ),
        # URL without a trailing slash remains unchanged
        (
            "https://relay.example.ts.net",
            {},
            {},
            {},
            "https://relay.example.ts.net",
        ),
    ],
)
def test_resolve_strips_trailing_slashes(scheduler, env, prof, server, expected):
    args = argparse.Namespace(scheduler=scheduler)

    relay_main.resolve(args, env, prof, server)

    assert args.scheduler == expected
