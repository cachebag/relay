import json
import plistlib
import xml.etree.ElementTree as ET
from pathlib import Path

import pytest

from relay import procs, profile, services


@pytest.fixture
def platform_service(tmp_path, monkeypatch):
    home = tmp_path / "home"
    config = home / "config" / "relay"
    config.mkdir(parents=True)
    binary = tmp_path / "binary with spaces" / "relayd.exe"
    binary.parent.mkdir()
    binary.write_bytes(b"fake relayd")
    monkeypatch.setattr(Path, "home", lambda: home)
    monkeypatch.setattr(profile, "config_dir", lambda: config)
    monkeypatch.setenv("XDG_CONFIG_HOME", str(home / "config"))
    monkeypatch.setattr(services, "_macos_domain", lambda: "gui/501")
    monkeypatch.setattr(services, "_owner", lambda: "scheduler-owner")
    monkeypatch.setattr(services, "_windows_owner", lambda: "S-1-5-21-123-456-789-1001")

    calls = []
    current = {"platform": "", "state": "inactive", "task": False}
    monkeypatch.setattr(services, "_platform", lambda: current["platform"])
    monkeypatch.setattr(
        services,
        "_windows_state",
        lambda: (
            "4"
            if current["task"] and current["state"] == "active"
            else "3"
            if current["task"]
            else "missing"
        ),
    )

    def run(argv, **kwargs):
        calls.append(argv)
        if argv[0] == "systemctl":
            if "show" in argv:
                return current["state"]
            if "enable" in argv or "start" in argv:
                current["state"] = "active"
            if "stop" in argv or "disable" in argv:
                current["state"] = "inactive"
        elif argv[0] == "/bin/launchctl":
            if "bootstrap" in argv:
                current["state"] = "active"
            elif "bootout" in argv:
                current["state"] = "inactive"
            elif "print" in argv:
                if current["state"] == "inactive":
                    raise services.ServiceError("Could not find service")
                return "state = running"
        elif argv[0] == "schtasks.exe":
            if "/Create" in argv:
                current["xml"] = Path(argv[-1]).read_bytes()
                current["task"] = True
            if "/Run" in argv:
                current["state"] = "active"
            if "/End" in argv:
                current["state"] = "inactive"
            if "/Delete" in argv:
                current["task"] = False
        return ""

    monkeypatch.setattr(services, "_run", run)
    return current, calls, binary, config


@pytest.mark.parametrize("platform", ["linux", "darwin", "win32"])
def test_install_start_stop_and_remove(platform_service, platform):
    current, calls, binary, config = platform_service
    current["platform"] = platform
    args = [str(binary), "--port", "9876", "--public", "--name", 'a $HOME % & "name"']
    key = 'tskey-auth-<&"%'

    services.install(args, key)
    assert services.installed()
    assert services.state() == "active"
    assert services.configuration() == [str(binary.resolve()), *args[1:]]
    assert services.log_offset() == 0

    if platform == "linux":
        unit = services._unit().read_text()
        assert services.LINUX_MARKER in unit
        assert "StartLimitIntervalSec=0" in unit
        assert "Restart=always" in unit and "RestartSec=10" in unit
        assert (
            "ExecStart="
            + " ".join(
                services._systemd_quote(arg.replace("$", "$$"))
                for arg in services.configuration()
            )
            in unit
        )
        assert "Environment=" + services._systemd_quote(f"TS_AUTHKEY={key}") in unit
        assert any(command[:2] == ["loginctl", "enable-linger"] for command in calls)
    elif platform == "darwin":
        plist = plistlib.loads(services._plist().read_bytes())
        assert plist["ProgramArguments"] == services.configuration()
        assert plist["EnvironmentVariables"]["TS_AUTHKEY"] == key
        assert plist["WorkingDirectory"] == str(config.resolve())
        assert plist["RunAtLoad"] and plist["KeepAlive"]
        assert "UserName" not in plist
        assert (
            services._plist()
            == Path.home() / "Library" / "LaunchAgents" / "relay.relayd.plist"
        )
        assert [
            "/bin/launchctl",
            "bootstrap",
            "gui/501",
            str(services._plist()),
        ] in calls
        assert ["/bin/launchctl", "print", "gui/501/relay.relayd"] in calls
        assert not any("sudo" in command or "system" in command for command in calls)
    else:
        import subprocess

        xml = ET.fromstring(current["xml"])
        ns = {"t": "http://schemas.microsoft.com/windows/2004/02/mit/task"}
        owner = "S-1-5-21-123-456-789-1001"
        assert xml.find("t:Triggers/t:BootTrigger", ns) is None
        assert (
            xml.findtext("t:Triggers/t:LogonTrigger/t:UserId", namespaces=ns) == owner
        )
        assert xml.findtext("t:Principals/t:Principal/t:UserId", namespaces=ns) == owner
        assert (
            xml.findtext("t:Principals/t:Principal/t:LogonType", namespaces=ns)
            == "InteractiveToken"
        )
        assert (
            xml.findtext("t:Principals/t:Principal/t:RunLevel", namespaces=ns)
            == "LeastPrivilege"
        )
        assert xml.findtext("t:Settings/t:ExecutionTimeLimit", namespaces=ns) == "PT0S"
        assert (
            xml.findtext("t:Settings/t:RestartOnFailure/t:Interval", namespaces=ns)
            == "PT1M"
        )
        assert (
            xml.findtext("t:Settings/t:RestartOnFailure/t:Count", namespaces=ns)
            == "255"
        )
        assert xml.findtext("t:Actions/t:Exec/t:Command", namespaces=ns) == str(
            binary.resolve()
        )
        assert xml.findtext(
            "t:Actions/t:Exec/t:Arguments", namespaces=ns
        ) == subprocess.list2cmdline(
            [
                *args[1:],
                "--config-dir",
                str(config.resolve()),
                "--log-file",
                str(procs.log_path("relayd").resolve()),
            ]
        )
        assert xml.findtext(
            "t:Actions/t:Exec/t:WorkingDirectory", namespaces=ns
        ) == str(config.resolve())
        assert key not in current["xml"].decode("utf-16")
        assert profile.server()["ts_authkey"] == key
        assert not list(config.glob("*.xml"))
        assert not list(config.glob("*.ps1"))
        assert not list(config.glob("*.exe"))

    assert services.stop()
    assert services.installed()
    assert services.state() == "inactive"
    services.start()
    assert services.state() == "active"
    services.remove()
    assert not services.installed()
    assert not services._marker().exists()
    assert procs.log_path("relayd").exists()
    if platform == "linux":
        assert not services._unit().exists()
        assert not any("disable-linger" in command for command in calls)
    elif platform == "darwin":
        assert not services._plist().exists()
    else:
        assert not current["task"]


def test_linux_skips_enable_linger_when_already_lingering(
    platform_service, monkeypatch
):
    current, calls, binary, _ = platform_service
    current["platform"] = "linux"
    fake = services._run

    def run(argv, **kwargs):
        if argv[:2] == ["loginctl", "show-user"]:
            calls.append(argv)
            return "yes"
        return fake(argv, **kwargs)

    monkeypatch.setattr(services, "_run", run)
    services.install([str(binary), "--port", "8080"], None)

    assert ["loginctl", "show-user", "scheduler-owner"] == calls[0][:3]
    assert not any(command[:2] == ["loginctl", "enable-linger"] for command in calls)
    assert services.installed()


def test_linux_says_how_to_enable_linger_when_refused(platform_service, monkeypatch):
    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    fake = services._run

    def run(argv, **kwargs):
        if argv[:2] == ["loginctl", "enable-linger"]:
            raise services.ServiceError("loginctl failed: Access denied")
        return fake(argv, **kwargs)

    monkeypatch.setattr(services, "_run", run)
    with pytest.raises(
        services.ServiceError, match="sudo loginctl enable-linger scheduler-owner"
    ):
        services.install([str(binary), "--port", "8080"], None)

    assert not services._unit().exists()
    assert not services.installed()


def test_linux_refuses_existing_unmanaged_unit(platform_service):
    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    unit = services._unit()
    unit.parent.mkdir(parents=True)
    unit.write_text("someone else's unit")
    with pytest.raises(services.ServiceError, match="not managed"):
        services.install([str(binary), "--port", "8080"], None)
    assert unit.read_text() == "someone else's unit"


def test_linux_refuses_to_remove_a_replaced_unit(platform_service):
    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    services.install([str(binary), "--port", "8080"], None)
    services._unit().write_text("a different service")
    with pytest.raises(services.ServiceError, match="was replaced"):
        services.installed()
    assert services._unit().read_text() == "a different service"


def test_linux_failed_activation_removes_registration(platform_service, monkeypatch):
    current, calls, binary, _ = platform_service
    current["platform"] = "linux"
    original = services._run

    def fail_enable(argv, **kwargs):
        if argv[:4] == ["systemctl", "--user", "enable", "--now"]:
            raise services.ServiceError("enable failed")
        return original(argv, **kwargs)

    monkeypatch.setattr(services, "_run", fail_enable)
    with pytest.raises(services.ServiceError, match="enable failed"):
        services.install([str(binary), "--port", "8080"], None)
    assert not services._unit().exists()
    assert not services._marker().exists()
    assert any("disable" in command for command in calls)


def test_windows_refuses_existing_task(platform_service):
    current, _, binary, _ = platform_service
    current["platform"] = "win32"
    current["task"] = True
    with pytest.raises(services.ServiceError, match="not managed"):
        services.install([str(binary), "--port", "8080"], None)
    assert current["task"]
    assert not services._marker().exists()


def test_windows_status_reports_task_state_without_supervisor(
    platform_service, monkeypatch
):
    current, _, binary, _ = platform_service
    current["platform"] = "win32"
    services.install([str(binary), "--port", "8080"], None)
    for task_state, expected in [
        ("4", "active"),
        ("2", "activating"),
        ("3", "inactive"),
        ("1", "inactive"),
        ("0", "failed"),
    ]:
        monkeypatch.setattr(services, "_windows_state", lambda value=task_state: value)
        assert services.state() == expected
    monkeypatch.setattr(services, "_windows_state", lambda: "missing")
    with pytest.raises(services.ServiceError, match="task is missing"):
        services.state()


def test_windows_stop_waits_until_task_stops(platform_service, monkeypatch):
    current, calls, binary, _ = platform_service
    current["platform"] = "win32"
    services.install([str(binary), "--port", "8080"], None)
    states = iter(["4", "4", "3"])
    sleeps = []
    monkeypatch.setattr(services, "_windows_state", lambda: next(states))
    monkeypatch.setattr(services.time, "sleep", sleeps.append)

    assert services.stop()
    assert sleeps == [0.1]
    assert any("/End" in command for command in calls)


def test_macos_reports_unknown_launchd_output(platform_service, monkeypatch):
    current, _, binary, _ = platform_service
    current["platform"] = "darwin"
    services.install([str(binary), "--port", "8080"], None)
    monkeypatch.setattr(services, "_run", lambda argv: "unexpected output")
    with pytest.raises(services.ServiceError, match="determine the launchd"):
        services.state()


def test_configuration_rejects_malformed_metadata(platform_service):
    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    services.install([str(binary), "--port", "8080"], None)
    services._marker().write_text(json.dumps({"platform": "linux", "argv": ["bad"]}))
    with pytest.raises(services.ServiceError, match="invalid saved"):
        services.configuration()


def test_service_output_does_not_restore_stale_public_address(platform_service):
    from relay.commands import scheduler

    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    procs.log_path("relayd").write_text(
        "server admin token: old-token\nrelayd public address: https://old.example\n"
    )
    services.install([str(binary), "--port", "8080"], None)
    with procs.log_path("relayd").open("a") as log:
        log.write("relayd listening on :8080\n")
    profile.save_server(public_url="")
    scheduler.capture_service_output()
    assert profile.server() == {"public_url": ""}


def test_service_output_after_log_truncation(platform_service):
    from relay.commands import scheduler

    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    procs.log_path("relayd").write_text("old output\n" * 100)
    services.install(
        [str(binary), "--port", "8080", "--public", "--name", "relay"], "tskey"
    )
    procs.log_path("relayd").write_text(
        "server admin token: new-token\nrelayd public address: https://new.example\n"
    )
    scheduler.capture_service_output()
    assert profile.server()["admin_token"] == "new-token"
    assert profile.server()["public_url"] == "https://new.example"


def test_service_start_replaces_stale_token_without_restoring_it_on_status(
    platform_service,
):
    from relay.commands import scheduler

    current, _, binary, _ = platform_service
    current["platform"] = "linux"
    services.install([str(binary), "--port", "8080"], None)
    profile.save_server(admin_token="stale-token")
    procs.log_path("relayd").write_text("server admin token: new-token\n")

    scheduler.capture_service_output()
    assert profile.server()["admin_token"] == "new-token"
    assert services.token_was_captured("new-token")

    profile.save_server(admin_token="rotated-token")
    scheduler.capture_service_output()
    assert profile.server()["admin_token"] == "rotated-token"


@pytest.mark.parametrize("platform", ["linux", "darwin"])
def test_missing_marker_names_cleanup_file_and_steps(platform_service, platform):
    current, _, binary, _ = platform_service
    current["platform"] = platform
    services.install([str(binary), "--port", "8080"], None)
    services._marker().unlink()
    registration = services._unit() if platform == "linux" else services._plist()
    with pytest.raises(services.ServiceError) as error:
        services.installed()
    message = str(error.value)
    assert str(registration) in message
    assert "delete" in message
    assert (
        "systemctl --user disable --now" in message
        if platform == "linux"
        else "launchctl bootout gui/501/relay.relayd" in message
    )
    assert registration.exists()


@pytest.mark.parametrize("platform", ["darwin", "win32"])
def test_failed_activation_rolls_back_registration(
    platform_service, monkeypatch, platform
):
    current, calls, binary, config = platform_service
    current["platform"] = platform
    original = services._run

    def fail_start(argv, **kwargs):
        if "bootstrap" in argv or "/Run" in argv:
            raise services.ServiceError("activation failed")
        return original(argv, **kwargs)

    monkeypatch.setattr(services, "_run", fail_start)
    with pytest.raises(services.ServiceError, match="activation failed"):
        services.install([str(binary), "--port", "8080"], None)
    assert not services._marker().exists()
    assert not list(config.glob("*.xml"))
    if platform == "darwin":
        assert not services._plist().exists()
    else:
        assert not current["task"]
        assert any("/Delete" in command for command in calls)


def test_windows_task_name_tracks_profile(platform_service, monkeypatch, tmp_path):
    _, _, _, config = platform_service
    first = services._task_name()
    monkeypatch.setattr(profile, "config_dir", lambda: tmp_path / "other-relay")
    assert services._task_name() != first
    monkeypatch.setattr(profile, "config_dir", lambda: config)
    assert services._task_name() == first


def test_windows_owner_rejects_unexpected_identity(monkeypatch):
    monkeypatch.setattr(services, "_powershell", lambda script: "unexpected output")
    with pytest.raises(services.ServiceError, match="current Windows account"):
        services._windows_owner()
