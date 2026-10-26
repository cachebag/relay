import argparse
import os
import sys
from pathlib import Path

import httpx
import pytest

from relay import binaries, profile
from relay.commands import install

_RealClient = httpx.Client


@pytest.fixture
def cfg(tmp_path, monkeypatch):
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "relay")
    monkeypatch.delenv("RELAYD", raising=False)
    monkeypatch.delenv("RELAY_AGENT", raising=False)
    # PATH setup writes to the home folder; keep it inside tmp_path.
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: tmp_path))
    monkeypatch.setenv("SHELL", "/bin/zsh")
    monkeypatch.setenv("PATH", "/usr/bin")
    return tmp_path / "relay"


@pytest.fixture
def releases(monkeypatch):
    """A fake GitHub: /releases/latest redirects to the v0.2.6 tag page, and
    assets under /releases/download/<tag>/ stream a small fake binary."""
    seen = []

    def handler(req: httpx.Request) -> httpx.Response:
        seen.append(str(req.url))
        path = req.url.path
        if path.endswith("/releases/latest"):
            return httpx.Response(
                302, headers={"location": f"{binaries.REPO}/releases/tag/v0.2.6"}
            )
        if path.endswith("/nope"):
            return httpx.Response(404)
        if "/releases/download/v0.2.6/" in path or "/releases/download/v0.2.1/" in path:
            body = b"#!/bin/sh\necho fake " + path.encode()
            return httpx.Response(
                200, content=body, headers={"content-length": str(len(body))}
            )
        return httpx.Response(404)

    monkeypatch.setattr(
        httpx,
        "Client",
        lambda **kw: _RealClient(
            transport=httpx.MockTransport(handler), follow_redirects=True
        ),
    )
    return seen


def test_asset_names():
    assert binaries.asset_name("relayd", "darwin", "arm64") == "relayd-darwin-arm64"
    assert (
        binaries.asset_name("relay-agent", "linux", "amd64")
        == "relay-agent-linux-amd64"
    )
    assert (
        binaries.asset_name("relay-agent", "windows", "amd64")
        == "relay-agent-windows-amd64.exe"
    )


def test_this_platform_maps_machine_names(monkeypatch):
    monkeypatch.setattr(binaries.platform, "system", lambda: "Darwin")
    monkeypatch.setattr(binaries.platform, "machine", lambda: "arm64")
    assert binaries.this_platform() == ("darwin", "arm64")
    monkeypatch.setattr(binaries.platform, "system", lambda: "Linux")
    monkeypatch.setattr(binaries.platform, "machine", lambda: "x86_64")
    assert binaries.this_platform() == ("linux", "amd64")
    monkeypatch.setattr(binaries.platform, "system", lambda: "Windows")
    monkeypatch.setattr(binaries.platform, "machine", lambda: "AMD64")
    assert binaries.this_platform() == ("windows", "amd64")


def test_install_downloads_both_into_config_bin(cfg, releases, monkeypatch, capsys):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("linux", "amd64"))
    install.run(argparse.Namespace(version=None, agent_only=False))
    out = capsys.readouterr().out
    for name in ("relayd", "relay-agent"):
        path = cfg / "bin" / name
        assert path.exists()
        assert b"fake" in path.read_bytes()
        if sys.platform != "win32":
            assert os.access(path, os.X_OK)
        assert "v0.2.6" in out
    assert "contribute this machine" in out and "run a scheduler" in out
    assert any(
        u.endswith("/releases/download/v0.2.6/relayd-linux-amd64") for u in releases
    )
    assert binaries.installed_versions() == {
        "relayd": "v0.2.6",
        "relay-agent": "v0.2.6",
    }


def test_install_agent_only_and_pinned_version(cfg, releases, monkeypatch):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("darwin", "arm64"))
    install.run(argparse.Namespace(version="0.2.1", agent_only=True))
    assert (cfg / "bin" / "relay-agent").exists()
    assert not (cfg / "bin" / "relayd").exists()
    assert releases == [
        f"{binaries.REPO}/releases/download/v0.2.1/relay-agent-darwin-arm64"
    ]


def test_install_unknown_version_exits_1(cfg, releases, monkeypatch, capsys):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("linux", "amd64"))
    with pytest.raises(SystemExit) as exc:
        install.run(argparse.Namespace(version="v9.9.9", agent_only=False))
    assert exc.value.code == 1
    assert "no release v9.9.9" in capsys.readouterr().err


def test_install_refuses_unpublished_platform(cfg, releases, monkeypatch, capsys):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("linux", "arm64"))
    with pytest.raises(SystemExit):
        install.run(argparse.Namespace(version=None, agent_only=False))
    assert "no release build for linux/arm64" in capsys.readouterr().err
    assert releases == []


def test_find_prefers_env_then_config_dir_then_path(cfg, monkeypatch, tmp_path):
    with pytest.raises(binaries.MissingBinary) as exc:
        binaries.find("relay-agent")
    assert "run relay install first" in str(exc.value)

    # Windows only finds executables with an extension from PATHEXT.
    exe = "relay-agent.exe" if sys.platform == "win32" else "relay-agent"
    on_path = tmp_path / "pathdir" / exe
    on_path.parent.mkdir()
    on_path.write_text("#!/bin/sh\n")
    on_path.chmod(0o755)
    monkeypatch.setenv("PATH", str(on_path.parent))
    assert binaries.find("relay-agent") == on_path

    local = cfg / "bin" / exe
    local.parent.mkdir(parents=True)
    local.write_text("#!/bin/sh\n")
    assert binaries.find("relay-agent") == local

    monkeypatch.setenv("RELAY_AGENT", "/opt/custom/relay-agent")
    assert binaries.find("relay-agent") == binaries.Path("/opt/custom/relay-agent")


def test_install_latest_missing_says_so(cfg, releases, monkeypatch, capsys):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("darwin", "amd64"))
    monkeypatch.setattr(binaries, "asset_name", lambda n, o, a: "nope")
    with pytest.raises(SystemExit):
        install.run(argparse.Namespace(version=None, agent_only=True))
    assert "not public" in capsys.readouterr().err


def test_install_skips_a_binary_already_at_that_version(
    cfg, releases, monkeypatch, capsys
):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("linux", "amd64"))
    install.run(argparse.Namespace(version=None, agent_only=True))
    downloads = [u for u in releases if "/download/" in u]
    assert len(downloads) == 1
    install.run(argparse.Namespace(version=None, agent_only=True))
    assert [u for u in releases if "/download/" in u] == downloads
    assert "already installed" in capsys.readouterr().out


def test_install_hint_quotes_a_path_with_spaces(
    cfg, releases, monkeypatch, capsys, tmp_path
):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("darwin", "arm64"))
    monkeypatch.setattr(
        profile, "config_dir", lambda: tmp_path / "Application Support" / "relay"
    )
    install.run(argparse.Namespace(version=None, agent_only=True, no_path=True))
    out = capsys.readouterr().out
    if sys.platform == "win32":
        want = f'"{tmp_path / "Application Support" / "relay" / "bin" / "relay-agent"}" --scheduler'
    else:
        want = '"$HOME/Application Support/relay/bin/relay-agent" --scheduler'
    assert want in out


def test_install_adds_bin_to_the_shell_rc(cfg, releases, monkeypatch, capsys, tmp_path):
    monkeypatch.setattr(binaries, "this_platform", lambda: ("darwin", "arm64"))
    install.run(argparse.Namespace(version=None, agent_only=True))
    out = capsys.readouterr().out
    assert "added to ~/.zshrc; restart your shell or run: source ~/.zshrc" in out
    assert "\n  relay-agent --scheduler" in out
    assert "or add" not in out
    rc = (tmp_path / ".zshrc").read_text()
    assert 'export PATH="$HOME/relay/bin:$PATH"' in rc
    install.run(argparse.Namespace(version=None, agent_only=True))
    assert (tmp_path / ".zshrc").read_text().count("relay/bin") == 1
    assert "already in ~/.zshrc" in capsys.readouterr().out
