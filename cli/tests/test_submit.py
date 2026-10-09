import json

import httpx

from relay.commands.submit import submit


def test_submit_posts_command():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/jobs"
        assert json.loads(request.content) == {"command": ["echo", "hi"]}
        return httpx.Response(201, json={"id": "abc123", "status": "queued"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    job = submit("http://scheduler", ["echo", "hi"], client=client)
    assert job["id"] == "abc123"
    assert job["status"] == "queued"
