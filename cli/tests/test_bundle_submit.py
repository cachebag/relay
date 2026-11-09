import argparse
import io
import json
import tarfile
from pathlib import Path

import httpx
import pytest

from relay.commands import status
from relay.commands import submit as submit_command
from relay.commands.submit import build_bundle, bundle_from_args, submit


def entries(data: bytes) -> dict[str, bytes | None]:
    """name -> content for files, None for folders."""
    out = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as tar:
        for m in tar.getmembers():
            out[m.name] = tar.extractfile(m).read() if m.isfile() else None
    return out


def make_job_dir(tmp_path: Path, entry: str = "run.sh") -> Path:
    root = tmp_path / "crunch"
    (root / "sub").mkdir(parents=True)
    (root / ".git").mkdir()
    # Bytes, not text: write_text would turn \n into \r\n on Windows.
    (root / entry).write_bytes(b"#!/bin/sh\necho hi\n")
    (root / "data.csv").write_bytes(b"1,2\n")
    (root / "sub" / "x.txt").write_bytes(b"x")
    (root / ".git" / "HEAD").write_bytes(b"ref")
    return root


def test_build_bundle_packs_folder_relative(tmp_path):
    """Paths are relative to the folder with forward slashes; .git is skipped."""
    root = make_job_dir(tmp_path)
    got = entries(build_bundle(root))
    assert got == {
        "data.csv": b"1,2\n",
        "run.sh": b"#!/bin/sh\necho hi\n",
        "sub": None,
        "sub/x.txt": b"x",
    }


def test_dir_uses_run_file_and_file_sets_entry(tmp_path):
    root = make_job_dir(tmp_path)
    bundle, entry = bundle_from_args(argparse.Namespace(dir=str(root)))
    assert entry is None
    assert "run.sh" in entries(bundle)

    bundle, entry = bundle_from_args(
        argparse.Namespace(dir=str(root), entry="data.csv")
    )
    assert entry == "data.csv"

    bundle, entry = bundle_from_args(argparse.Namespace(file=str(root / "data.csv")))
    assert entry == "data.csv"
    assert entries(bundle) == {"data.csv": b"1,2\n"}


def test_bad_bundle_args_exit_with_a_reason(tmp_path, capsys):
    root = make_job_dir(tmp_path, entry="task.py")
    cases = [
        (argparse.Namespace(dir=str(root)), "no run.sh, run.ps1 or run.cmd"),
        (argparse.Namespace(dir=str(root), entry="nope.py"), "nope.py is not in"),
        (argparse.Namespace(dir=str(root), file="x"), "not both"),
        (argparse.Namespace(entry="task.py"), "--entry needs --dir"),
        (argparse.Namespace(file=str(root / "missing")), "is not a file"),
        (argparse.Namespace(dir=str(root / "missing")), "is not a folder"),
    ]
    for args, msg in cases:
        with pytest.raises(SystemExit) as exc:
            bundle_from_args(args)
        assert exc.value.code == 2
        assert msg in capsys.readouterr().err


def test_bundle_over_the_cap_is_refused(tmp_path, monkeypatch, capsys):
    root = make_job_dir(tmp_path)
    monkeypatch.setattr(submit_command, "MAX_BUNDLE_BYTES", 10)
    with pytest.raises(SystemExit) as exc:
        bundle_from_args(argparse.Namespace(dir=str(root)))
    assert exc.value.code == 2
    assert "the limit is 0 MB" in capsys.readouterr().err


def test_submit_with_bundle_posts_multipart():
    """The JSON goes in a job part and the tar in a bundle part."""
    seen = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["content_type"] = request.headers["Content-Type"]
        seen["body"] = request.content
        return httpx.Response(201, json={"id": "j1", "status": "queued"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    job = submit(
        "http://scheduler/",
        ["--fast"],
        "tok",
        "alice",
        client=client,
        bundle=b"TARBYTES",
        entry="run.sh",
    )
    assert job["id"] == "j1"
    assert seen["content_type"].startswith("multipart/form-data")
    body = seen["body"]
    assert b'name="job"' in body
    assert (
        json.dumps(
            {"command": ["--fast"], "submitter": "alice", "entry": "run.sh"}
        ).encode()
        in body
    )
    assert b'name="bundle"; filename="job.tgz"' in body
    assert b"TARBYTES" in body


def test_submit_without_bundle_is_still_plain_json():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers["Content-Type"] == "application/json"
        assert json.loads(request.content) == {"command": ["x"], "submitter": "a"}
        return httpx.Response(201, json={"id": "j1", "status": "queued"})

    client = httpx.Client(transport=httpx.MockTransport(handler))
    submit("http://scheduler", ["x"], "tok", "a", client=client)


def test_run_needs_a_command_without_files(capsys):
    with pytest.raises(SystemExit) as exc:
        submit_command.run(argparse.Namespace(scheduler="http://s", command=[]))
    assert exc.value.code == 2
    assert "command is required" in capsys.readouterr().err


def test_run_prints_the_schedulers_reason_for_a_bad_bundle(
    tmp_path, monkeypatch, capsys
):
    from relay import profile

    root = make_job_dir(tmp_path)
    monkeypatch.setattr(profile, "load", lambda: {"pool_token": "t", "name": "a"})

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(413, text="job files must be 50 MB or less\n")

    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(httpx, "Client", lambda **kw: client)
    with pytest.raises(SystemExit) as exc:
        submit_command.run(
            argparse.Namespace(scheduler="http://s", command=[], dir=str(root))
        )
    assert exc.value.code == 1
    assert "relay: job files must be 50 MB or less" in capsys.readouterr().err


def test_describe_shows_entry_for_bundle_jobs():
    assert status.describe({"command": ["echo", "hi"]}) == "echo hi"
    assert (
        status.describe({"command": ["--fast"], "entry": "run.sh", "bundle_bytes": 10})
        == "run.sh --fast"
    )
    assert status.describe({"command": [], "bundle_bytes": 10}) == "run.*"
