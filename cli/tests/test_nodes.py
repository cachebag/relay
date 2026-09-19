import httpx

from relay.commands.nodes import fetch, seconds_ago


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
    nodes = fetch("http://scheduler", client=client)
    assert len(nodes) == 1
    assert nodes[0]["hostname"] == "node-a"


def test_seconds_ago_is_positive_for_past():
    assert seconds_ago("2020-01-01T00:00:00Z") > 0
