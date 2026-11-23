"""IT018: a saved join token must show actionable guidance after rotation."""

import argparse

import httpx
import pytest

from relay import profile
from relay.commands import nodes, submit


@pytest.fixture
def saved_profile(tmp_path, monkeypatch):
    """Use a real on-disk profile, never the contributor's actual cli.json."""
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    profile.save(
        {
            "scheduler": "http://test",
            "pool_id": "pool1",
            "pool_name": "lab",
            "pool_token": "previous-join-token",
            "name": "alice",
        }
    )
    return profile.path().read_bytes()


@pytest.mark.parametrize(
    "command,args,endpoint",
    [
        (nodes, argparse.Namespace(scheduler="http://test"), "/nodes"),
        (
            submit,
            argparse.Namespace(scheduler="http://test", command=["echo", "hello"]),
            "/jobs",
        ),
    ],
    ids=["nodes", "submit"],
)
@pytest.mark.parametrize(
    "response_text,expected",
    [
        (
            "pool token was rotated; ask the admin for the new invite\n",
            "pool token was rotated",
        ),
        ("invalid pool token\n", "invalid pool token"),
    ],
    ids=["rotated", "invalid"],
)
def test_it018_cli_403_message_and_unchanged_profile(
    saved_profile, monkeypatch, capsys, command, args, endpoint, response_text, expected
):
    seen = []

    def handler(request):
        seen.append(
            (request.method, request.url.path, request.headers["Authorization"])
        )
        return httpx.Response(403, text=response_text)

    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)

    with pytest.raises(SystemExit) as exc:
        command.run(args)

    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert captured.out == ""
    assert expected in captured.err
    assert "previous-join-token" not in captured.err
    if expected == "pool token was rotated":
        assert "ask the admin for the new invite" in captured.err
        assert "relay login" in captured.err
        assert "invalid pool token" not in captured.err
    else:
        assert "pool token was rotated" not in captured.err

    assert seen == [
        (
            "GET" if endpoint == "/nodes" else "POST",
            endpoint,
            "Bearer previous-join-token",
        )
    ]
    assert profile.path().read_bytes() == saved_profile


def test_it018_nodes_other_http_errors_still_raise(saved_profile, monkeypatch):
    """The authentication guidance must not mask server-side failures."""
    client = httpx.Client(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(503, text="service unavailable")
        )
    )
    monkeypatch.setattr(httpx, "Client", lambda **kwargs: client)

    with pytest.raises(httpx.HTTPStatusError) as exc:
        nodes.run(argparse.Namespace(scheduler="http://test"))
    assert exc.value.response.status_code == 503
    assert profile.path().read_bytes() == saved_profile
