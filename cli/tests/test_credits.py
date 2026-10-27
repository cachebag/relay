import argparse

import httpx
import pytest
from relay import profile
from relay.commands import credits

_RealClient = httpx.Client

PROFILE = {
    "scheduler": "http://scheduler",
    "pool_id": "961dda5133b6",
    "pool_name": "lab",
    "pool_token": "tok",
    "name": "omar",
}


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")


def test_credits_prints_table_and_marks_user(cfg, monkeypatch, capsys):
    profile.save(PROFILE)

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/pools/961dda5133b6/credits"
        assert request.headers["Authorization"] == "Bearer tok"
        return httpx.Response(
            200,
            json=[
                {"account": "akrm", "balance": 42},
                {"account": "omar", "balance": 10},
            ],
        )

    monkeypatch.setattr(
        credits.httpx,
        "Client",
        lambda **kw: _RealClient(transport=httpx.MockTransport(handler)),
    )
    credits.run(argparse.Namespace(scheduler="http://scheduler"))

    lines = capsys.readouterr().out.splitlines()
    assert lines[0].split() == ["ACCOUNT", "BALANCE"]
    assert lines[1].split() == ["akrm", "42"]
    assert lines[2].split() == ["*", "omar", "10"]


def test_credits_not_logged_in(cfg, capsys):
    with pytest.raises(SystemExit) as exc:
        credits.run(argparse.Namespace(scheduler="http://scheduler"))
    assert exc.value.code == 1
    assert "not logged in, run relay login first" in capsys.readouterr().err
