import json

import pytest

from relay import profile

LAB = {
    "scheduler": "http://a",
    "pool_id": "p1",
    "pool_name": "lab",
    "pool_token": "t1",
    "name": "akrm",
}
HOME = {
    "scheduler": "http://b",
    "pool_id": "p2",
    "pool_name": "home",
    "pool_token": "t2",
    "name": "akrm",
}


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    return tmp_path / "relay" / profile.FILENAME


def test_old_single_login_file_still_loads(cfg):
    cfg.parent.mkdir()
    cfg.write_text(json.dumps(LAB))
    assert profile.load() == LAB
    assert profile.logins() == [LAB]


def test_login_adds_and_switches(cfg):
    profile.save(LAB)
    profile.save(HOME)
    assert profile.load() == HOME
    assert profile.logins() == [LAB, HOME]
    # Logging in to the same pool again replaces its entry, e.g. a rotated token.
    profile.save({**LAB, "pool_token": "t1b"})
    assert profile.load()["pool_token"] == "t1b"
    assert len(profile.logins()) == 2


def test_use_by_name_or_id(cfg):
    profile.save(LAB)
    profile.save(HOME)
    assert profile.use("lab") == LAB
    assert profile.load() == LAB
    assert profile.use("p2") == HOME
    with pytest.raises(KeyError):
        profile.use("nope")


def test_delete_falls_back_to_the_next_login(cfg):
    profile.save(LAB)
    profile.save(HOME)
    assert profile.delete() == LAB
    assert profile.load() == LAB
    assert profile.delete() == {}
    assert not cfg.exists()
    assert profile.delete() == {}


def test_server_settings_survive_logging_out(cfg):
    assert profile.server() == {}
    profile.save_server(admin_token="sat", address="http://localhost:8080")
    profile.save_server(public_url="https://relay.x.ts.net")
    assert profile.server() == {
        "admin_token": "sat",
        "address": "http://localhost:8080",
        "public_url": "https://relay.x.ts.net",
    }
    profile.save(LAB)
    assert profile.load() == LAB
    profile.delete()
    assert profile.load() == {}
    assert profile.server()["admin_token"] == "sat"
    assert cfg.exists()
