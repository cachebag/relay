import httpx

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
    nodes = fetch("http://scheduler", client=client)
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
    nodes = fetch("http://scheduler", client=client)
    assert status(nodes[0]["last_seen"]) == "offline"


def test_fetch_sends_pool_query_param():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.params["pool"] == "abc"
        return httpx.Response(200, json=[])

    client = httpx.Client(transport=httpx.MockTransport(handler))
    assert fetch("http://scheduler", pool="abc", client=client) == []
