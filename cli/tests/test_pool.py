import json

import httpx
from relay.commands.pool import create


def test_create_returns_pool():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/pools"
        assert json.loads(request.content)["name"] == "csc4996"
        return httpx.Response(200, json={"id": "pool-123", "token": "tok-abc"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    pool = create("http://scheduler", "csc4996", client=client)
    assert pool["id"] == "pool-123"
    assert pool["token"] == "tok-abc"
