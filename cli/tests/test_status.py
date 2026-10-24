import httpx

from relay.commands.status import fetch


def test_fetch_returns_job():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/jobs/abc123"
        assert request.headers["Authorization"] == "Bearer tok"
        return httpx.Response(
            200,
            json={
                "id": "abc123",
                "command": ["echo", "hi"],
                "status": "queued",
                "node_id": "",
                "attempts": 0,
                "created_at": "2026-09-23T15:01:03.320292Z",
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    job = fetch("http://scheduler", "abc123", "tok", client=client)
    assert job["status"] == "queued"
    assert job["command"] == ["echo", "hi"]


def test_requires_os_displayed():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/jobs/abc123"
        assert request.headers["Authorization"] == "Bearer tok"
        return httpx.Response(
            200,
            json={
                "id": "abc123",
                "command": ["echo", "hi"],
                "status": "done",
                "node_id": "node-1",
                "attempts": 1,
                "created_at": "2026-09-23T15:01:03.320292Z",
                "requires": {"os": "linux"},
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    job = fetch("http://scheduler", "abc123", "tok", client=client)
    assert job["requires"]["os"] == "linux"


def test_no_os_requirement():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/jobs/abc123"
        assert request.headers["Authorization"] == "Bearer tok"
        return httpx.Response(
            200,
            json={
                "id": "abc123",
                "command": ["echo", "hi"],
                "status": "done",
                "node_id": "node-1",
                "attempts": 1,
                "created_at": "2026-09-23T15:01:03.320292Z",
            },
        )

    client = httpx.Client(transport=httpx.MockTransport(handler))
    job = fetch("http://scheduler", "abc123", "tok", client=client)
    assert "requires" not in job