from datetime import UTC, datetime
from types import SimpleNamespace

import httpx

import relay.commands.nodes as nodes_command
from relay.commands.nodes import fetch, seconds_ago, status


def test_fetch_returns_nodes():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/nodes"
        return httpx.Response(
            200,
            json=[
                {
                    "hostname": "node-a",
                    "os": "linux",
                    "arch": "amd64",
                    "cpus": 8,
                    "mem_bytes": 16_000_000_000,
                    "last_seen": "2026-09-15T14:00:00Z",
                }
            ],
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    nodes = fetch("http://scheduler", "tok", client=client)
    assert len(nodes) == 1
    assert nodes[0]["hostname"] == "node-a"


def test_seconds_ago_is_positive_for_past():
    assert seconds_ago("2020-01-01T00:00:00Z") > 0


def test_old_node_is_offline():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json=[
                {
                    "hostname": "node-b",
                    "os": "linux",
                    "arch": "amd64",
                    "cpus": 4,
                    "mem_bytes": 8_000_000_000,
                    "last_seen": "2020-01-01T00:00:00Z",
                }
            ],
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    nodes = fetch("http://scheduler", "tok", client=client)
    assert status(nodes[0]["last_seen"]) == "offline"


def test_fetch_sends_join_token():
    """The join token goes in the Authorization header; no pool parameter."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer abc"
        assert "pool" not in request.url.params
        return httpx.Response(200, json=[])

    client = httpx.Client(transport=httpx.MockTransport(handler))
    assert fetch("http://scheduler", token="abc", client=client) == []

 
# This test mocks the fetch function to return a node with a specific ID and checks
# that the run command includes this ID in its output.
def test_run_prints_node_id(monkeypatch, capsys):
    monkeypatch.setattr(
        nodes_command,
        "fetch",
        lambda *_args: [
            {
                "id": "a1b2c3d4e5f6",
                "hostname": "shared-host",
                "status": "online",
                "os": "linux",
                "arch": "amd64",
                "cpus": 4,
                "mem_bytes": 8_000_000_000,
                "last_seen": datetime.now(UTC).isoformat(),
            }
        ],
    )

    monkeypatch.setattr(
        nodes_command.profile,
        "require_login",
        lambda: {"pool_token": "test-token"},
    )
    nodes_command.run(SimpleNamespace(scheduler="http://scheduler", pool=None))

    output = capsys.readouterr().out
    header, row = output.splitlines()
    assert "ID" in header
    assert "a1b2c3d4e5f6" in row
