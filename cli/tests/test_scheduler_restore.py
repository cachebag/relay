import argparse
import sqlite3
from contextlib import closing

import httpx
import pytest

from relay import procs, profile, services
from relay.__main__ import resolve
from relay.commands import scheduler

CORE_SCHEMA = """
    CREATE TABLE nodes (
        id TEXT PRIMARY KEY, hostname TEXT, os TEXT, arch TEXT,
        cpus INTEGER, mem_bytes INTEGER, last_seen INTEGER
    );
    CREATE TABLE pools (
        id TEXT PRIMARY KEY, name TEXT, token TEXT, created_at INTEGER
    );
    CREATE TABLE jobs (
        id TEXT PRIMARY KEY, command TEXT, status TEXT,
        node_id TEXT, attempts INTEGER, created_at INTEGER
    );
"""


def make_database(path, value="backed-up", legacy=False):
    with closing(sqlite3.connect(path)) as db:
        db.executescript(CORE_SCHEMA)
        db.execute(
            "INSERT INTO nodes VALUES ('node-1', ?, 'linux', 'amd64', 4, 1024, 123)",
            (value,),
        )
        db.execute("INSERT INTO pools VALUES ('pool-1', ?, 'join-token', 123)", (value,))
        db.execute(
            "INSERT INTO jobs VALUES ('job-1', ?, 'queued', '', 0, 123)", (value,)
        )
        if not legacy:
            db.executescript("""
                CREATE TABLE credits (
                    id TEXT PRIMARY KEY, pool_id TEXT, account TEXT,
                    delta INTEGER, reason TEXT, job_id TEXT, created_at INTEGER
                );
                CREATE TABLE server (key TEXT PRIMARY KEY, value TEXT);
                INSERT INTO credits VALUES (
                    'credit-1', 'pool-1', 'alice', 7, 'earned', '', 123
                );
                INSERT INTO server VALUES ('admin_token_hash', 'saved-hash');
            """)
        db.commit()


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    directory = tmp_path / "relay"
    directory.mkdir()
    monkeypatch.setattr(profile, "config_dir", lambda: directory)
    monkeypatch.setattr(procs, "running", lambda name: None)
    monkeypatch.setattr(services, "installed", lambda: False)
    monkeypatch.delenv("RELAY_ADMIN_TOKEN", raising=False)
    return directory


class StreamBody(httpx.SyncByteStream):
    def __init__(self, body):
        self.body = body

    def __iter__(self):
        yield self.body


def mock_client(monkeypatch, handler):
    client = httpx.Client(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(scheduler.httpx, "Client", lambda **kwargs: client)


def backup_args(tmp_path, **updates):
    args = argparse.Namespace(
        to=tmp_path / "backup.db",
        scheduler="http://localhost:8080",
        scheduler_given=False,
    )
    for name, value in updates.items():
        setattr(args, name, value)
    return args


def test_backup_environment_token_overrides_saved_token(cfg, tmp_path, monkeypatch):
    profile.save_server(admin_token="stale-token", address="http://localhost:8080")
    monkeypatch.setenv("RELAY_ADMIN_TOKEN", "current-token")
    requests = []

    def handler(request):
        requests.append(request)
        return httpx.Response(200, stream=StreamBody(b"backup bytes"))

    mock_client(monkeypatch, handler)
    args = backup_args(tmp_path)
    scheduler.run_backup(args)

    assert requests[0].headers["Authorization"] == "Bearer current-token"
    assert args.to.read_bytes() == b"backup bytes"


@pytest.mark.parametrize("public_url", ["", "https://local.example"])
def test_backup_implicit_target_uses_local_server(
    cfg, tmp_path, monkeypatch, public_url
):
    server = profile.save_server(
        admin_token="local-token", address="http://localhost:9123", public_url=public_url
    )
    args = backup_args(tmp_path, scheduler=None)
    resolve(args, {}, {"scheduler": "https://remote.example"}, server)
    assert args.scheduler == "https://remote.example"
    requests = []

    def handler(request):
        requests.append(request)
        return httpx.Response(200, stream=StreamBody(b"backup bytes"))

    mock_client(monkeypatch, handler)
    scheduler.run_backup(args)

    expected = public_url or "http://localhost:9123"
    assert str(requests[0].url) == f"{expected}/admin/backup"


def test_backup_explicit_target_is_honored(cfg, tmp_path, monkeypatch):
    server = profile.save_server(
        admin_token="local-token", address="http://localhost:9123"
    )
    args = backup_args(tmp_path, scheduler="https://remote.example")
    resolve(args, {}, {}, server)
    requests = []

    def handler(request):
        requests.append(request)
        return httpx.Response(200, stream=StreamBody(b"backup bytes"))

    mock_client(monkeypatch, handler)
    scheduler.run_backup(args)

    assert str(requests[0].url) == "https://remote.example/admin/backup"


@pytest.mark.parametrize("status", [401, 500, 503])
def test_backup_reports_http_error(cfg, tmp_path, monkeypatch, status):
    profile.save_server(admin_token="admin-token")
    mock_client(
        monkeypatch,
        lambda request: httpx.Response(status, stream=StreamBody(b"backup failed\n")),
    )
    args = backup_args(tmp_path)

    with pytest.raises(SystemExit) as exc:
        scheduler.run_backup(args)

    message = str(exc.value)
    if status == 401:
        assert "server admin token was rejected" in message
    else:
        assert str(status) in message
        assert "backup failed" in message
    assert not args.to.exists()
    assert not args.to.with_name(f"{args.to.name}.part").exists()


@pytest.mark.parametrize("kind", ["empty", "text", "truncated", "unrelated"])
def test_restore_rejects_invalid_backup(cfg, tmp_path, kind):
    source = tmp_path / "backup.db"
    if kind == "empty":
        source.write_bytes(b"")
    elif kind == "text":
        source.write_bytes(b"not a SQLite database")
    elif kind == "truncated":
        make_database(source)
        source.write_bytes(source.read_bytes()[:512])
    else:
        with closing(sqlite3.connect(source)) as db:
            db.execute("CREATE TABLE unrelated (id INTEGER)")

    destination = cfg / "relay.db"
    make_database(destination, "original")
    wal = cfg / "relay.db-wal"
    shm = cfg / "relay.db-shm"
    wal.write_bytes(b"original WAL")
    shm.write_bytes(b"original SHM")
    original = destination.read_bytes()

    with pytest.raises(SystemExit):
        scheduler.run_restore(argparse.Namespace(file=source))

    assert destination.read_bytes() == original
    assert wal.read_bytes() == b"original WAL"
    assert shm.read_bytes() == b"original SHM"
    assert not (cfg / "relay.db.restoring").exists()


def test_restore_refuses_still_stopping_service(cfg, tmp_path, monkeypatch):
    source = tmp_path / "backup.db"
    make_database(source)
    destination = cfg / "relay.db"
    make_database(destination, "original")
    original = destination.read_bytes()
    monkeypatch.setattr(services, "installed", lambda: True)
    monkeypatch.setattr(services, "state", lambda: "deactivating")

    with pytest.raises(SystemExit) as exc:
        scheduler.run_restore(argparse.Namespace(file=source))

    assert "service is running" in str(exc.value)
    assert destination.read_bytes() == original


def test_restore_preserves_backup(cfg, tmp_path):
    source = tmp_path / "backup.db"
    make_database(source)
    destination = cfg / "relay.db"
    make_database(destination, "original")
    (cfg / "relay.db-wal").write_bytes(b"old WAL")
    (cfg / "relay.db-shm").write_bytes(b"old SHM")

    scheduler.run_restore(argparse.Namespace(file=source))

    assert not (cfg / "relay.db-wal").exists()
    assert not (cfg / "relay.db-shm").exists()
    assert not (cfg / "relay.db.restoring").exists()
    with closing(sqlite3.connect(destination)) as db:
        assert db.execute("SELECT hostname FROM nodes").fetchall() == [("backed-up",)]
        assert db.execute("SELECT name, token FROM pools").fetchall() == [
            ("backed-up", "join-token")
        ]
        assert db.execute("SELECT command FROM jobs").fetchall() == [("backed-up",)]
        assert db.execute("SELECT account, delta FROM credits").fetchall() == [
            ("alice", 7)
        ]
        assert db.execute("SELECT key, value FROM server").fetchall() == [
            ("admin_token_hash", "saved-hash")
        ]


def test_restore_accepts_legacy_database(cfg, tmp_path):
    source = tmp_path / "backup.db"
    make_database(source, legacy=True)

    scheduler.run_restore(argparse.Namespace(file=source))

    with closing(sqlite3.connect(cfg / "relay.db")) as db:
        assert db.execute("SELECT name FROM pools").fetchall() == [("backed-up",)]
        assert db.execute(
            "SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name"
        ).fetchall() == [("jobs",), ("nodes",), ("pools",)]
