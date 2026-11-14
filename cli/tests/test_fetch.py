import io
import tarfile
from types import SimpleNamespace

import httpx
import pytest

from relay.commands import fetch

JOB = {
    "id": "j1",
    "status": "done",
    "output_files": [
        {"path": "result.csv", "bytes": 9},
        {"path": "logs/run.txt", "bytes": 2},
    ],
}
FILES = {"result.csv": b"total,55\n", "logs/run.txt": b"ok"}


@pytest.fixture(autouse=True)
def logged_in(monkeypatch, tmp_path):
    """Every test acts as a logged-in member working in an empty folder."""
    monkeypatch.setattr(fetch.profile, "load", lambda: {"pool_token": "tok"})
    monkeypatch.chdir(tmp_path)


def bundle(files: dict[str, bytes]) -> bytes:
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        for name, data in files.items():
            info = tarfile.TarInfo(name)
            info.size = len(data)
            tar.addfile(info, io.BytesIO(data))
    return buf.getvalue()


def scheduler(job: dict = JOB, files: dict[str, bytes] = FILES):
    """A client for a fake scheduler, and the paths it was asked for."""
    asked = []

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers["Authorization"] == "Bearer tok"
        asked.append(request.url.path)
        if request.url.path == "/jobs/j1":
            return httpx.Response(200, json=job)
        if request.url.path == "/jobs/j1/output/files":
            return httpx.Response(200, content=bundle(files))
        return httpx.Response(404)

    return httpx.Client(transport=httpx.MockTransport(handler)), asked


def args(job_id: str = "j1", to: str | None = None, force: bool = False):
    return SimpleNamespace(scheduler="http://test", id=job_id, to=to, force=force)


def test_run_unpacks_into_relay_out(tmp_path, capsys) -> None:
    client, asked = scheduler()

    fetch.run(args(), client=client)

    dest = tmp_path / "relay-out" / "j1"
    assert (dest / "result.csv").read_bytes() == b"total,55\n"
    assert (dest / "logs" / "run.txt").read_bytes() == b"ok"
    assert asked == ["/jobs/j1", "/jobs/j1/output/files"]
    out = capsys.readouterr().out.splitlines()
    assert out[-1].startswith("2 files in ")
    assert any(line.endswith("result.csv") for line in out)


def test_run_unpacks_into_the_folder_given_with_to(tmp_path) -> None:
    client, _ = scheduler()

    fetch.run(args(to=str(tmp_path / "here")), client=client)

    assert (tmp_path / "here" / "result.csv").exists()
    assert not (tmp_path / "relay-out").exists()


def test_run_does_not_overwrite_without_force(tmp_path, capsys) -> None:
    client, _ = scheduler()
    dest = tmp_path / "relay-out" / "j1"
    dest.mkdir(parents=True)
    (dest / "result.csv").write_bytes(b"mine")

    with pytest.raises(SystemExit) as exc:
        fetch.run(args(), client=client)

    assert exc.value.code == 1
    assert "use --force to overwrite" in capsys.readouterr().err
    assert (dest / "result.csv").read_bytes() == b"mine"
    # Nothing is written when any file is in the way.
    assert not (dest / "logs").exists()


def test_run_overwrites_with_force(tmp_path) -> None:
    client, _ = scheduler()
    dest = tmp_path / "relay-out" / "j1"
    dest.mkdir(parents=True)
    (dest / "result.csv").write_bytes(b"mine")

    fetch.run(args(force=True), client=client)

    assert (dest / "result.csv").read_bytes() == b"total,55\n"


def test_run_says_so_when_the_job_has_no_files(tmp_path, capsys) -> None:
    client, asked = scheduler(job={"id": "j1", "status": "done"})

    fetch.run(args(), client=client)

    assert capsys.readouterr().out == "job j1 has no output files\n"
    assert asked == ["/jobs/j1"]
    assert not (tmp_path / "relay-out").exists()


def test_run_exits_one_when_the_job_has_not_finished(capsys) -> None:
    client, _ = scheduler(job={"id": "j1", "status": "running"})

    with pytest.raises(SystemExit) as exc:
        fetch.run(args(), client=client)

    assert exc.value.code == 1
    assert "has not finished" in capsys.readouterr().err


def test_run_exits_one_for_an_unknown_job(capsys) -> None:
    client, _ = scheduler()

    with pytest.raises(SystemExit) as exc:
        fetch.run(args(job_id="nope"), client=client)

    assert exc.value.code == 1
    assert "no such job" in capsys.readouterr().err


def test_unpack_rejects_a_path_outside_the_folder(tmp_path) -> None:
    with pytest.raises(fetch.FetchFailed, match="unsafe path"):
        fetch.unpack(bundle({"../evil.txt": b"x"}), tmp_path / "dest")

    assert not (tmp_path / "evil.txt").exists()


def test_unpack_reports_where_an_absolute_path_was_written(tmp_path) -> None:
    dest = tmp_path / "dest"

    written = fetch.unpack(bundle({"/abs/result.csv": b"x"}), dest)

    assert written == [dest / "abs" / "result.csv"]
    assert written[0].read_bytes() == b"x"


def test_unpack_rejects_something_that_is_not_a_tar(tmp_path) -> None:
    with pytest.raises(fetch.FetchFailed, match="not a valid tar"):
        fetch.unpack(b"hello", tmp_path / "dest")
