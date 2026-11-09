import json
import plistlib
import shutil
import sys
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
    monkeypatch.setenv("PROGRAMDATA", str(tmp_path / "Program Data"))
    monkeypatch.setattr(services, "_plist", lambda: tmp_path / "relay.relayd.plist")
    monkeypatch.setattr(services, "_authorize_macos", lambda: None)
    monkeypatch.setattr(services, "_authorize_windows", lambda: None)
    monkeypatch.setattr(services, "_owner", lambda: "scheduler-owner")
    monkeypatch.setattr(services, "_windows_check_state", lambda: None)
    monkeypatch.setattr(
        services,
        "_windows_dir",
        lambda: tmp_path / "Program Data" / services._task_name(),
    )
    monkeypatch.setattr(
        services, "_windows_private_dir", lambda path: path.mkdir(parents=True)
    )

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
            if current["state"] == "inactive":
                raise services.ServiceError("Could not find service")
            return "state = running"
        elif argv[0] == "schtasks.exe":
            if "/Create" in argv:
                current["task"] = True
            if "/Run" in argv:
                current["state"] = "active"
                services._windows_runtime_state().write_text("running")
            if "/End" in argv:
                current["state"] = "inactive"
            if "/Delete" in argv:
                current["task"] = False
        return ""

    def privileged(argv):
        calls.append(argv)
        if argv[0] == "/usr/bin/install":
            shutil.copyfile(argv[-2], argv[-1])
        elif argv[0] == "/bin/rm":
            Path(argv[-1]).unlink(missing_ok="-f" in argv)
        elif "bootstrap" in argv:
            current["state"] = "active"
        elif "bootout" in argv:
            current["state"] = "inactive"
        elif "print" in argv:
            if current["state"] == "inactive":
                raise services.ServiceError("Could not find service")
            return "state = running"
        return ""

    monkeypatch.setattr(services, "_run", run)
    monkeypatch.setattr(services, "_privileged", privileged)
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
        assert any("bootstrap" in command for command in calls)
    else:
        directory = services._windows_dir()
        launch = json.loads((directory / "launch.json").read_text())
        assert launch["arguments"] == args[1:]
        assert Path(launch["binary"]) == directory / "relayd.exe"
        assert launch["environment"]["TS_AUTHKEY"] == key
        assert launch["environment"]["APPDATA"] == str(config.parent.resolve())
        assert launch["workingDirectory"] == str(config.resolve())
        assert (directory / "relayd.exe").read_bytes() == binary.read_bytes()
        assert "AssignProcessToJobObject" in (directory / "supervisor.ps1").read_text()
        xml = ET.fromstring((directory / "task.xml").read_bytes())
        ns = {"t": "http://schemas.microsoft.com/windows/2004/02/mit/task"}
        assert xml.find("t:Triggers/t:BootTrigger", ns) is not None
        assert (
            xml.findtext("t:Principals/t:Principal/t:UserId", namespaces=ns)
            == "S-1-5-18"
        )
        assert xml.findtext("t:Settings/t:ExecutionTimeLimit", namespaces=ns) == "PT0S"
        assert key not in (directory / "task.xml").read_text(encoding="utf-16")

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
        assert not services._windows_dir().exists()


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
    assert not services._windows_dir().exists()


def test_windows_status_distinguishes_supervisor_from_running_daemon(platform_service):
    current, _, binary, _ = platform_service
    current["platform"] = "win32"
    services.install([str(binary), "--port", "8080"], None)
    state_file = services._windows_runtime_state()
    state_file.unlink()
    assert services.state() == "activating"
    state_file.write_text("starting")
    assert services.state() == "activating"
    state_file.write_text("running")
    assert services.state() == "active"
    state_file.write_text("unexpected")
    with pytest.raises(services.ServiceError, match="runtime state"):
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
    monkeypatch.setattr(services, "_privileged", lambda argv: "unexpected output")
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


@pytest.mark.skipif(sys.platform != "win32", reason="Windows known folders")
def test_windows_service_directory_ignores_programdata_override(tmp_path, monkeypatch):
    directory = services._windows_dir()
    monkeypatch.setenv("PROGRAMDATA", str(tmp_path))
    assert services._windows_dir() == directory


@pytest.mark.skipif(sys.platform != "win32", reason="Windows ACLs")
def test_windows_state_acl_validation_accepts_private_directory(tmp_path, monkeypatch):
    script = r"""
$owner = [Security.Principal.WindowsIdentity]::GetCurrent().User
$directory = [IO.DirectoryInfo]::new($env:RELAY_TEST_DIR)
$acl = [Security.AccessControl.DirectorySecurity]::new()
$acl.SetAccessRuleProtection($true, $false)
foreach ($sid in @($owner.Value, 'S-1-5-18', 'S-1-5-32-544')) {
    $identity = [Security.Principal.SecurityIdentifier]::new($sid)
    $rule = [Security.AccessControl.FileSystemAccessRule]::new(
        $identity, 'FullControl', 'ContainerInherit, ObjectInherit', 'None', 'Allow')
    $acl.AddAccessRule($rule)
}
$directory.SetAccessControl($acl)
"""
    services._powershell(script, {"RELAY_TEST_DIR": str(tmp_path)})
    config = tmp_path / "relay"
    config.mkdir()
    (config / "relay.db").write_bytes(b"test data")
    monkeypatch.setattr(Path, "home", lambda: tmp_path)
    monkeypatch.setattr(profile, "config_dir", lambda: config)
    services._windows_check_state()


@pytest.mark.skipif(sys.platform != "win32", reason="Windows supervisor")
def test_windows_supervisor_restarts_and_stops_its_child(tmp_path):
    import ctypes
    import subprocess
    import time

    log = tmp_path / "service.log"
    script = tmp_path / "supervisor.ps1"
    script.write_text(
        services.WINDOWS_SUPERVISOR.replace(
            "Start-Sleep -Seconds 10", "Start-Sleep -Milliseconds 100"
        )
    )
    counter = tmp_path / "count"
    special_argument = 'argument with spaces, $ % & "'
    key = 'tskey-auth-special-$%"'
    code = (
        "import os,sys,time,pathlib; "
        f"assert os.environ['TS_AUTHKEY'] == {key!r}; "
        f"assert sys.argv[1] == {special_argument!r}; "
        f"p=pathlib.Path({str(counter)!r}); "
        "n=int(p.read_text())+1 if p.exists() else 1; "
        "p.write_text(str(n)); "
        "print('relayd listening on :8080', file=sys.stderr, flush=True); "
        "print('started '+str(os.getpid()), flush=True); "
        "time.sleep(.3 if n == 1 else 30)"
    )
    (tmp_path / "launch.json").write_text(
        json.dumps(
            {
                "binary": sys.executable,
                "arguments": ["-c", code, special_argument],
                "workingDirectory": str(tmp_path),
                "environment": {"TS_AUTHKEY": key},
                "log": str(log),
                "runtimeState": str(tmp_path / "runtime-state"),
            }
        )
    )
    process = subprocess.Popen(
        [
            "powershell.exe",
            "-NoProfile",
            "-NonInteractive",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(script),
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    child_pid = None
    try:
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            lines = log.read_text().splitlines() if log.exists() else []
            started = [line for line in lines if line.startswith("started ")]
            if len(started) >= 2:
                child_pid = int(started[-1].split()[-1])
                break
            if process.poll() is not None:
                break
            time.sleep(0.1)
        assert child_pid is not None, (
            process.communicate(timeout=5)
            if process.poll() is not None
            else "supervisor did not restart"
        )
    finally:
        process.terminate()
        process.wait(timeout=10)

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = [ctypes.c_ulong, ctypes.c_int, ctypes.c_ulong]
    kernel.OpenProcess.restype = ctypes.c_void_p
    kernel.GetExitCodeProcess.argtypes = [
        ctypes.c_void_p,
        ctypes.POINTER(ctypes.c_ulong),
    ]
    kernel.CloseHandle.argtypes = [ctypes.c_void_p]
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        handle = kernel.OpenProcess(0x1000, False, child_pid)
        if not handle:
            break
        exit_code = ctypes.c_ulong()
        assert kernel.GetExitCodeProcess(handle, ctypes.byref(exit_code))
        kernel.CloseHandle(handle)
        if exit_code.value != 259:
            break
        time.sleep(0.1)
    else:
        pytest.fail("the supervised child survived stopping the supervisor")
