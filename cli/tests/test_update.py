import argparse
import io
import sqlite3
import tarfile
from contextlib import closing

import httpx
import pytest

from relay import binaries, contribution, procs, profile, services
from relay.commands import scheduler, update

_RealClient = httpx.Client
LATEST = "v0.4.0"


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    folder = tmp_path / "relay"
    monkeypatch.setattr(profile, "config_dir", lambda: folder)
    monkeypatch.setattr(binaries, "this_platform", lambda: ("linux", "amd64"))
    monkeypatch.setattr(services, "installed", lambda: False)
    monkeypatch.setattr(update, "version", lambda name: "0.4.0")
    return folder


@pytest.fixture
def releases(monkeypatch):
    """A fake GitHub whose latest release is v0.4.0. Each binary's bytes name
    the tag it came from."""
    seen = []

    def handler(req: httpx.Request) -> httpx.Response:
        seen.append(str(req.url))
        path = req.url.path
        if path.endswith("/releases/latest"):
            return httpx.Response(
                302, headers={"location": f"{binaries.REPO}/releases/tag/{LATEST}"}
            )
        tag = path.split("/releases/download/")[1].split("/")[0]
        if tag not in (LATEST, "v0.3.2"):
            return httpx.Response(404)
        name = path.rsplit("/", 1)[1][: -len(".tar.gz")].rsplit("-", 2)[0]
        body = f"{name} {tag}".encode()
        info = tarfile.TarInfo(name)
        info.size = len(body)
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz") as tar:
            tar.addfile(info, io.BytesIO(body))
        return httpx.Response(200, content=buf.getvalue())

    monkeypatch.setattr(
        httpx,
        "Client",
        lambda **kw: _RealClient(
            transport=httpx.MockTransport(handler), follow_redirects=True
        ),
    )
    return seen


def installed(cfg, *names, tag="v0.3.2"):
    for name in names:
        binaries.download(name, tag)
    return cfg / "bin"


def run(version=None, check=False):
    update.run(argparse.Namespace(version=version, check=check))


def downloads(seen):
    return [u for u in seen if "/releases/download/" in u]


def leftovers(bin_dir):
    return [p.name for p in bin_dir.iterdir() if p.suffix in (".new", ".old", ".part")]


class Relayd:
    """A relayd that is running outside the service, with the start, stop
    and health calls update makes recorded in order."""

    def __init__(self, monkeypatch, cfg, starts=True):
        self.cfg = cfg
        self.running = True
        self.starts = starts
        self.events = []
        self.flags = []
        cfg.mkdir(parents=True, exist_ok=True)
        (cfg / "relayd.argv.json").write_text(
            '["/bin/relayd", "--port", "9123", "--public", "--name", "lab"]'
        )
        with closing(sqlite3.connect(cfg / "relay.db")) as db:
            db.executescript("""
                CREATE TABLE nodes (id, hostname, os, arch, cpus, mem_bytes, last_seen);
                CREATE TABLE pools (id, name, token, created_at);
                CREATE TABLE jobs (id, command, status, node_id, attempts, created_at);
                INSERT INTO pools VALUES ('p1', 'lab', 'jt', 1);
            """)
        monkeypatch.setattr(procs, "running", lambda name: 77 if self.running else None)
        monkeypatch.setattr(procs, "stop", self.stop)
        monkeypatch.setattr(scheduler, "run_start", self.start)
        monkeypatch.setattr(update, "healthy", lambda address: self.running)

    def stop(self, name):
        self.events.append(("stop", self.binary()))
        self.running = False
        return True

    def binary(self):
        return (self.cfg / "bin" / "relayd").read_text()

    def start(self, flags):
        self.events.append(("start", self.binary()))
        self.flags.append(flags)
        if LATEST in self.binary():
            # What a migration does on first start.
            with closing(sqlite3.connect(self.cfg / "relay.db")) as db:
                db.execute("INSERT INTO pools VALUES ('p2', 'migrated', 'x', 2)")
                db.commit()
            if not self.starts:
                raise SystemExit("relayd stopped right away")
        self.running = True

    def pools(self):
        with closing(sqlite3.connect(self.cfg / "relay.db")) as db:
            return [row[0] for row in db.execute("SELECT id FROM pools ORDER BY id")]


def test_update_needs_installed_binaries(cfg, releases):
    with pytest.raises(SystemExit) as exc:
        run()
    assert "run relay install first" in str(exc.value)
    assert releases == []


def test_update_at_the_latest_release_does_nothing(cfg, releases, capsys):
    bin_dir = installed(cfg, "relay-agent", tag=LATEST)
    releases.clear()
    run()
    assert "relay-agent already at v0.4.0" in capsys.readouterr().out
    assert downloads(releases) == []
    assert leftovers(bin_dir) == []


def test_check_says_what_would_change_and_changes_nothing(cfg, releases, capsys):
    bin_dir = installed(cfg, "relayd", "relay-agent")
    releases.clear()
    run(check=True)
    out = capsys.readouterr().out
    assert "relayd: v0.3.2 -> v0.4.0" in out
    assert "relay-agent: v0.3.2 -> v0.4.0" in out
    assert downloads(releases) == []
    assert "v0.3.2" in (bin_dir / "relayd").read_text()


def test_update_only_touches_what_is_installed(cfg, releases, monkeypatch, capsys):
    bin_dir = installed(cfg, "relay-agent")
    monkeypatch.setattr(contribution, "statuses", list)
    monkeypatch.setattr(contribution, "stop_all", list)
    run()
    assert "relay-agent v0.4.0" in (bin_dir / "relay-agent").read_text()
    assert not (bin_dir / "relayd").exists()
    assert binaries.installed_versions() == {"relay-agent": LATEST}
    assert leftovers(bin_dir) == []
    assert "relay-agent updated to v0.4.0" in capsys.readouterr().out


def test_update_restarts_relayd_the_way_it_was_running(
    cfg, releases, monkeypatch, capsys
):
    bin_dir = installed(cfg, "relayd")
    relayd = Relayd(monkeypatch, cfg)
    run()
    # Stopped on the old binary, started on the new one.
    assert relayd.events == [("stop", "relayd v0.3.2"), ("start", "relayd v0.4.0")]
    flags = relayd.flags[0]
    assert (flags.port, flags.public, flags.name, flags.service) == (
        9123,
        True,
        "lab",
        False,
    )
    assert binaries.installed_versions() == {"relayd": LATEST}
    assert leftovers(bin_dir) == []
    assert "relayd updated to v0.4.0" in capsys.readouterr().out
    # The copy from before the migration is kept.
    backup = cfg / "backups" / "relay-before-v0.4.0.db"
    with closing(sqlite3.connect(backup)) as db:
        assert db.execute("SELECT id FROM pools").fetchall() == [("p1",)]
    assert relayd.pools() == ["p1", "p2"]


def test_update_of_a_stopped_relayd_leaves_it_stopped(
    cfg, releases, monkeypatch, capsys
):
    bin_dir = installed(cfg, "relayd")
    relayd = Relayd(monkeypatch, cfg)
    relayd.running = False
    run()
    assert relayd.events == []
    assert "relayd v0.4.0" in (bin_dir / "relayd").read_text()
    assert "it was not running" in capsys.readouterr().out
    assert (cfg / "backups" / "relay-before-v0.4.0.db").exists()


def test_a_relayd_that_does_not_start_is_rolled_back(
    cfg, releases, monkeypatch, capsys
):
    bin_dir = installed(cfg, "relayd", "relay-agent")
    relayd = Relayd(monkeypatch, cfg, starts=False)
    agents = []
    monkeypatch.setattr(contribution, "stop_all", lambda: agents.append("stopped"))
    with pytest.raises(SystemExit) as exc:
        run()
    assert relayd.events == [
        ("stop", "relayd v0.3.2"),
        ("start", "relayd v0.4.0"),
        ("stop", "relayd v0.4.0"),
        ("start", "relayd v0.3.2"),
    ]
    assert relayd.running
    assert "relayd v0.3.2" in (bin_dir / "relayd").read_text()
    # The half-migrated database is gone too.
    assert relayd.pools() == ["p1"]
    message = str(exc.value)
    assert "relayd v0.4.0 did not start" in message
    assert "running v0.3.2 again" in message
    assert "relay-before-v0.4.0.db" in message
    # The agent is left alone when the scheduler could not be updated.
    assert agents == []
    assert binaries.installed_versions() == {
        "relayd": "v0.3.2",
        "relay-agent": "v0.3.2",
    }
    assert leftovers(bin_dir) == []


def test_update_restarts_the_agents_that_were_running(
    cfg, releases, monkeypatch, capsys
):
    bin_dir = installed(cfg, "relay-agent")
    profile.save(
        {"scheduler": "http://s", "pool_id": "p1", "pool_name": "lab", "name": "me"}
    )
    running = [
        {"pid": 1, "pool_id": "p1", "pool_name": "lab", "scheduler": "http://s"},
        {"pid": 2, "pool_id": "gone", "pool_name": "old", "scheduler": "http://t"},
    ]
    events = []
    monkeypatch.setattr(contribution, "statuses", lambda: running)
    monkeypatch.setattr(
        contribution,
        "stop_all",
        lambda: events.append(("stop", (bin_dir / "relay-agent").read_text())),
    )

    def start(prof, address):
        events.append(("start", prof["pool_id"], address))
        events.append((bin_dir / "relay-agent").read_text())
        return 99, cfg / "relay-agent.p1.log"

    monkeypatch.setattr(contribution, "start", start)
    with pytest.raises(SystemExit):
        run()
    assert events == [
        ("stop", "relay-agent v0.3.2"),
        ("start", "p1", "http://s"),
        "relay-agent v0.4.0",
    ]
    captured = capsys.readouterr()
    assert "contributing to lab again (PID 99)" in captured.out
    # A pool this machine has since logged out of can't be restarted.
    assert "no longer contributing to old" in captured.err
    assert "relay contribute --pool old" in captured.err


def test_a_failed_download_stops_nothing(cfg, releases, monkeypatch):
    bin_dir = installed(cfg, "relayd")
    relayd = Relayd(monkeypatch, cfg)
    with pytest.raises(SystemExit) as exc:
        run(version="v9.9.9")
    assert "no release v9.9.9" in str(exc.value)
    assert relayd.events == []
    assert relayd.running
    assert "v0.3.2" in (bin_dir / "relayd").read_text()
    assert leftovers(bin_dir) == []


def test_an_older_cli_is_told_how_to_update(cfg, releases, monkeypatch, capsys):
    installed(cfg, "relay-agent", tag=LATEST)
    monkeypatch.setattr(update, "version", lambda name: "0.3.2")
    run()
    out = capsys.readouterr().out
    assert "this CLI is 0.3.2; update it with: uv tool upgrade relay" in out


def test_running_flags_fall_back_to_the_saved_address_and_the_log(cfg):
    cfg.mkdir(parents=True)
    profile.save_server(address="http://localhost:9000")
    flags = scheduler.running_flags()
    assert (flags.port, flags.public, flags.name) == (9000, False, "relay")

    (cfg / "relayd.log").write_text(
        "relayd listening on :9000\n"
        "relayd public address: https://lab.tail1234.ts.net\n"
    )
    flags = scheduler.running_flags()
    assert (flags.port, flags.public, flags.name) == (9000, True, "lab")
