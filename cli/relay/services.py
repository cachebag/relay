"""Install and control the scheduler's startup service."""

import hashlib
import json
import os
import plistlib
import subprocess
import sys
import tempfile
import time
import xml.etree.ElementTree as ET
from pathlib import Path

from relay import procs, profile

NAME = "relayd"
LABEL = "relay.relayd"
MARKER = "relayd-service.json"
LINUX_MARKER = "# Managed by relay scheduler"


class ServiceError(Exception):
    pass


def _platform() -> str:
    if sys.platform in ("linux", "darwin", "win32"):
        return sys.platform
    raise ServiceError(f"services are not supported on {sys.platform}")


def _marker() -> Path:
    return profile.config_dir() / MARKER


def _owner() -> str:
    import pwd

    try:
        return pwd.getpwuid(os.getuid()).pw_name
    except (KeyError, OSError) as exc:
        raise ServiceError("cannot determine the scheduler owner's account") from exc


def _unit() -> Path:
    base = os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config"
    return Path(base) / "systemd" / "user" / "relayd.service"


def _plist() -> Path:
    return Path.home() / "Library" / "LaunchAgents" / "relay.relayd.plist"


def _macos_domain() -> str:
    return f"gui/{os.getuid()}"


def _windows_id() -> str:
    path = str(profile.config_dir().resolve()).casefold().encode("utf-8")
    return hashlib.sha256(path).hexdigest()[:16]


def _task_name() -> str:
    return f"Relay-relayd-{_windows_id()}"


def _run(argv: list[str], *, env: dict | None = None) -> str:
    try:
        result = subprocess.run(
            argv, capture_output=True, text=True, env=env, check=False
        )
    except OSError as exc:
        raise ServiceError(f"could not run {argv[0]}: {exc}") from exc
    if result.returncode:
        detail = result.stderr.strip() or result.stdout.strip()
        raise ServiceError(f"{argv[0]} failed: {detail or result.returncode}")
    return result.stdout.strip()


def _write_private(path: Path, content: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, "wb") as file:
            file.write(content)
    except OSError:
        path.unlink(missing_ok=True)
        raise


def _metadata() -> dict:
    try:
        data = json.loads(_marker().read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise ServiceError(
            f"cannot read service registration {_marker()}: {exc}"
        ) from exc
    if not isinstance(data, dict) or data.get("platform") != _platform():
        raise ServiceError(f"invalid service registration: {_marker()}")
    return data


def installed() -> bool:
    platform = _platform()
    if _marker().exists():
        _metadata()
        if platform == "linux":
            try:
                first_line = _unit().read_text(encoding="utf-8").splitlines()[0]
            except FileNotFoundError as exc:
                raise ServiceError(
                    f"service registration is missing: {_unit()}"
                ) from exc
            except (OSError, UnicodeError, IndexError) as exc:
                raise ServiceError(
                    f"cannot read service registration: {_unit()}"
                ) from exc
            if first_line != LINUX_MARKER:
                raise ServiceError(f"service registration was replaced: {_unit()}")
        if platform == "darwin" and not _plist().exists():
            raise ServiceError(f"service registration is missing: {_plist()}")
        if platform == "win32" and _windows_state() == "missing":
            raise ServiceError("the registered Windows task is missing")
        return True
    if platform == "linux" and _unit().exists():
        raise ServiceError(
            f"existing service is not managed by this Relay profile: {_unit()}; "
            f"to remove it, run systemctl --user disable --now relayd.service, "
            f"delete {_unit()}, then run systemctl --user daemon-reload"
        )
    if platform == "darwin" and _plist().exists():
        raise ServiceError(
            f"existing service is not managed by this Relay profile: {_plist()}; "
            f"to remove it, run launchctl bootout {_macos_domain()}/{LABEL}, "
            f"then delete {_plist()}"
        )
    return False


def _systemd_quote(value: str) -> str:
    value = value.replace("\\", "\\\\").replace('"', '\\"')
    value = value.replace("%", "%%").replace("\n", "\\n").replace("\r", "\\r")
    return f'"{value}"'


def _environment(key: str | None) -> dict[str, str]:
    env = {"HOME": str(Path.home())}
    if _platform() == "linux":
        env["XDG_CONFIG_HOME"] = str(profile.config_dir().parent.resolve())
    if key is not None:
        env["TS_AUTHKEY"] = key
    return env


def _linux_unit(argv: list[str], env: dict[str, str]) -> bytes:
    lines = [
        LINUX_MARKER,
        "[Unit]",
        "Description=Relay scheduler",
        "StartLimitIntervalSec=0",
        "",
        "[Service]",
        "Type=simple",
        "WorkingDirectory=" + _systemd_quote(str(profile.config_dir().resolve())),
        "ExecStart=" + " ".join(_systemd_quote(arg.replace("$", "$$")) for arg in argv),
    ]
    lines.extend(
        "Environment=" + _systemd_quote(f"{name}={value}")
        for name, value in env.items()
    )
    log = str(procs.log_path(NAME).resolve()).replace("%", "%%")
    lines.extend(
        [
            "StandardOutput=append:" + log,
            "StandardError=append:" + log,
            "Restart=always",
            "RestartSec=10",
            "",
            "[Install]",
            "WantedBy=default.target",
            "",
        ]
    )
    return "\n".join(lines).encode()


def _macos_plist(argv: list[str], env: dict[str, str]) -> bytes:
    data = {
        "Label": LABEL,
        "ProgramArguments": argv,
        "WorkingDirectory": str(profile.config_dir().resolve()),
        "EnvironmentVariables": env,
        "RunAtLoad": True,
        "KeepAlive": True,
        "ThrottleInterval": 10,
        "StandardOutPath": str(procs.log_path(NAME).resolve()),
        "StandardErrorPath": str(procs.log_path(NAME).resolve()),
    }
    return plistlib.dumps(data)


def _powershell(script: str, extra_env: dict[str, str] | None = None) -> str:
    env = dict(os.environ)
    if extra_env:
        env.update(extra_env)
    return _run(
        ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script],
        env=env,
    )


def _windows_state() -> str:
    script = r"""
$ErrorActionPreference = 'Stop'
$service = New-Object -ComObject 'Schedule.Service'
$service.Connect()
try {
    $task = $service.GetFolder('\').GetTask($env:RELAY_TASK_NAME)
    [string]$task.State
} catch {
    $exception = $_.Exception
    while ($null -ne $exception) {
        if ($exception.HResult -eq -2147024894) { 'missing'; exit 0 }
        $exception = $exception.InnerException
    }
    throw
}
"""
    state = _powershell(script, {"RELAY_TASK_NAME": _task_name()})
    if state not in {"missing", "0", "1", "2", "3", "4"}:
        raise ServiceError(f"unexpected Windows task state: {state}")
    return state


def _windows_owner() -> str:
    sid = _powershell("[Security.Principal.WindowsIdentity]::GetCurrent().User.Value")
    if not sid.startswith("S-1-") or not all(
        part.isdigit() for part in sid[2:].split("-")
    ):
        raise ServiceError("cannot determine the current Windows account")
    return sid


def _windows_xml(argv: list[str], owner: str) -> bytes:
    ns = "http://schemas.microsoft.com/windows/2004/02/mit/task"
    ET.register_namespace("", ns)
    task = ET.Element(f"{{{ns}}}Task", version="1.3")

    def child(parent, name, value=None, **attrs):
        element = ET.SubElement(parent, f"{{{ns}}}{name}", attrs)
        element.text = value
        return element

    registration = child(task, "RegistrationInfo")
    child(registration, "Author", "Relay scheduler")
    triggers = child(task, "Triggers")
    trigger = child(triggers, "LogonTrigger")
    child(trigger, "Enabled", "true")
    child(trigger, "UserId", owner)
    principals = child(task, "Principals")
    principal = child(principals, "Principal", id="RelayUser")
    child(principal, "UserId", owner)
    child(principal, "LogonType", "InteractiveToken")
    child(principal, "RunLevel", "LeastPrivilege")
    settings = child(task, "Settings")
    for name, value in (
        ("MultipleInstancesPolicy", "IgnoreNew"),
        ("DisallowStartIfOnBatteries", "false"),
        ("StopIfGoingOnBatteries", "false"),
        ("AllowHardTerminate", "true"),
        ("StartWhenAvailable", "true"),
        ("AllowStartOnDemand", "true"),
        ("Enabled", "true"),
        ("ExecutionTimeLimit", "PT0S"),
    ):
        child(settings, name, value)
    restart = child(settings, "RestartOnFailure")
    child(restart, "Interval", "PT1M")
    child(restart, "Count", "255")
    actions = child(task, "Actions", Context="RelayUser")
    action = child(actions, "Exec")
    child(action, "Command", argv[0])
    child(
        action,
        "Arguments",
        subprocess.list2cmdline(
            [
                *argv[1:],
                "--config-dir",
                str(profile.config_dir().resolve()),
                "--log-file",
                str(procs.log_path(NAME).resolve()),
            ]
        ),
    )
    child(action, "WorkingDirectory", str(profile.config_dir().resolve()))
    return ET.tostring(task, encoding="utf-16", xml_declaration=True)


def _prepare_log() -> None:
    path = procs.log_path(NAME)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    os.close(fd)
    if _platform() != "win32":
        path.chmod(0o600)


def install(argv: list[str], key: str | None) -> None:
    platform = _platform()
    if installed():
        raise ServiceError("service is already installed")
    if not Path(argv[0]).is_file():
        raise ServiceError(f"relayd binary does not exist: {argv[0]}")
    argv = [str(Path(argv[0]).resolve()), *argv[1:]]
    profile.config_dir().mkdir(parents=True, exist_ok=True)
    if platform == "win32" and _windows_state() != "missing":
        raise ServiceError(
            f"existing task is not managed by this Relay profile: {_task_name()}; "
            f"to remove it, end it in Task Scheduler and delete task {_task_name()}"
        )
    _prepare_log()
    log_offset = procs.log_path(NAME).stat().st_size
    env = _environment(key)
    metadata = json.dumps(
        {"platform": platform, "argv": argv, "log_offset": log_offset}
    ).encode()
    created = False
    registered = False
    marked = False
    temporary = None
    try:
        if platform == "linux":
            _write_private(_unit(), _linux_unit(argv, env))
            created = True
            _write_private(_marker(), metadata)
            marked = True
            _run(["loginctl", "enable-linger", _owner()])
            _run(["systemctl", "--user", "daemon-reload"])
            registered = True
            _run(["systemctl", "--user", "enable", "--now", "relayd.service"])
        elif platform == "darwin":
            _write_private(_plist(), _macos_plist(argv, env))
            created = True
            _write_private(_marker(), metadata)
            marked = True
            _run(["/bin/launchctl", "bootstrap", _macos_domain(), str(_plist())])
            registered = True
        else:
            # The task runs as this user, so relayd reads their saved key from cli.json.
            if key is not None:
                profile.save_server(ts_authkey=key)
            xml = _windows_xml(argv, _windows_owner())
            with tempfile.NamedTemporaryFile(
                dir=profile.config_dir(), suffix=".xml", delete=False
            ) as file:
                temporary = Path(file.name)
                file.write(xml)
            _write_private(_marker(), metadata)
            marked = True
            _run(
                ["schtasks.exe", "/Create", "/TN", _task_name(), "/XML", str(temporary)]
            )
            registered = True
            _run(["schtasks.exe", "/Run", "/TN", _task_name()])
    except (OSError, ServiceError) as exc:
        failures = []
        if registered:
            try:
                if platform == "linux":
                    _run(["systemctl", "--user", "disable", "--now", "relayd.service"])
                elif platform == "darwin":
                    _run(["/bin/launchctl", "bootout", _macos_domain(), str(_plist())])
                else:
                    if _windows_state() in {"2", "4"}:
                        _run(["schtasks.exe", "/End", "/TN", _task_name()])
                    _run(["schtasks.exe", "/Delete", "/TN", _task_name(), "/F"])
            except ServiceError as rollback:
                failures.append(str(rollback))
        if created and not failures:
            try:
                if platform == "linux":
                    _unit().unlink(missing_ok=True)
                    _run(["systemctl", "--user", "daemon-reload"])
                elif platform == "darwin":
                    _plist().unlink(missing_ok=True)
            except (OSError, ServiceError) as rollback:
                failures.append(str(rollback))
        if marked and not failures:
            _marker().unlink(missing_ok=True)
        detail = f"; cleanup failed: {'; '.join(failures)}" if failures else ""
        raise ServiceError(f"could not install service: {exc}{detail}") from exc
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def state() -> str:
    platform = _platform()
    if platform == "linux":
        result = _run(
            [
                "systemctl",
                "--user",
                "show",
                "relayd.service",
                "--property=ActiveState",
                "--value",
            ]
        )
        if result not in {
            "active",
            "activating",
            "inactive",
            "failed",
            "deactivating",
            "reloading",
        }:
            raise ServiceError(f"unexpected systemd service state: {result}")
        return result
    if platform == "darwin":
        try:
            output = _run(["/bin/launchctl", "print", f"{_macos_domain()}/{LABEL}"])
        except ServiceError as exc:
            if "Could not find service" in str(exc):
                return "inactive"
            raise
        for line in output.splitlines():
            if line.strip() == "state = running":
                return "active"
            if line.strip() in {"state = waiting", "state = not running"}:
                return "activating"
        raise ServiceError("could not determine the launchd service state")
    result = _windows_state()
    if result == "missing":
        raise ServiceError("the registered Windows task is missing")
    if result == "4":
        return "active"
    if result == "2":
        return "activating"
    if result in {"1", "3"}:
        return "inactive"
    return "failed"


def start() -> None:
    if state() in {"active", "activating"}:
        return
    platform = _platform()
    if platform == "linux":
        _run(["systemctl", "--user", "start", "relayd.service"])
    elif platform == "darwin":
        _run(["/bin/launchctl", "bootstrap", _macos_domain(), str(_plist())])
    else:
        _run(["schtasks.exe", "/Run", "/TN", _task_name()])


def stop() -> bool:
    if state() == "inactive":
        return False
    platform = _platform()
    if platform == "linux":
        _run(["systemctl", "--user", "stop", "relayd.service"])
    elif platform == "darwin":
        _run(["/bin/launchctl", "bootout", _macos_domain(), str(_plist())])
    else:
        _run(["schtasks.exe", "/End", "/TN", _task_name()])
        deadline = time.monotonic() + 10
        while True:
            task_state = _windows_state()
            if task_state in {"1", "3"}:
                break
            if task_state == "missing":
                raise ServiceError("the registered Windows task is missing")
            if time.monotonic() >= deadline:
                raise ServiceError("the Windows service did not stop in time")
            time.sleep(0.1)
    return True


def remove() -> None:
    try:
        platform = _platform()
        if platform == "linux":
            _run(["systemctl", "--user", "disable", "--now", "relayd.service"])
            _unit().unlink()
            _run(["systemctl", "--user", "daemon-reload"])
        elif platform == "darwin":
            stop()
            _plist().unlink()
        else:
            stop()
            _run(["schtasks.exe", "/Delete", "/TN", _task_name(), "/F"])
        _marker().unlink()
    except OSError as exc:
        raise ServiceError(f"could not remove service files: {exc}") from exc


def configuration() -> list[str]:
    argv = _metadata().get("argv")
    if not isinstance(argv, list) or not all(isinstance(arg, str) for arg in argv):
        raise ServiceError("invalid saved service command line")
    if len(argv) not in (3, 6) or argv[1] != "--port":
        raise ServiceError("invalid saved service command line")
    if len(argv) == 6 and argv[3:5] != ["--public", "--name"]:
        raise ServiceError("invalid saved service command line")
    return argv


def log_offset() -> int:
    offset = _metadata().get("log_offset")
    if not isinstance(offset, int) or offset < 0:
        raise ServiceError("invalid saved service log position")
    return offset


def token_was_captured(token: str) -> bool:
    digest = hashlib.sha256(token.encode()).hexdigest()
    return _metadata().get("admin_token_digest") == digest


def remember_captured_token(token: str) -> None:
    data = _metadata()
    data["admin_token_digest"] = hashlib.sha256(token.encode()).hexdigest()
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=_marker().parent, delete=False) as file:
            temporary = Path(file.name)
            file.write(json.dumps(data).encode())
        if _platform() != "win32":
            temporary.chmod(0o600)
        os.replace(temporary, _marker())
    except OSError as exc:
        raise ServiceError(f"cannot update service registration: {exc}") from exc
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
