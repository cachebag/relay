import httpx
from relay.commands import jobs


def test_fetch_sends_pool_when_given() -> None:
    """jobs.fetch should send the pool ID as a query parameter."""

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/jobs"
        assert request.url.params.get("pool") == "pool-123"

        return httpx.Response(200, json=[])

    client = httpx.Client(transport=httpx.MockTransport(handler))

    result = jobs.fetch(
        "http://test",
        pool="pool-123",
        client=client,
    )

    assert result == []
