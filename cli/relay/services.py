"""Install and control the scheduler's boot service."""

import ctypes
import hashlib
import json
import os
import plistlib
import shutil
import subprocess
import sys
import tempfile
import time
import uuid
import xml.etree.ElementTree as ET
from pathlib import Path

from relay import procs, profile

NAME = "relayd"
LABEL = "relay.relayd"
MARKER = "relayd-service.json"
LINUX_MARKER = "# Managed by relay scheduler"
_macos_authorized = False


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
    return Path("/Library/LaunchDaemons/relay.relayd.plist")


def _windows_id() -> str:
    path = str(profile.config_dir().resolve()).casefold().encode("utf-8")
    return hashlib.sha256(path).hexdigest()[:16]


def _task_name() -> str:
    return f"Relay-relayd-{_windows_id()}"


def _windows_dir() -> Path:
    from ctypes import wintypes

    folder_id = ctypes.create_string_buffer(
        uuid.UUID("62ab5d82-fdc1-4dc3-a9dd-070d1d495d97").bytes_le
    )
    path = wintypes.LPWSTR()
    shell = ctypes.WinDLL("shell32", use_last_error=True)
    get_folder = shell.SHGetKnownFolderPath
    get_folder.argtypes = [
        wintypes.LPVOID,
        wintypes.DWORD,
        wintypes.HANDLE,
        ctypes.POINTER(wintypes.LPWSTR),
    ]
    get_folder.restype = ctypes.c_long
    result = get_folder(ctypes.byref(folder_id), 0, None, ctypes.byref(path))
    if result:
        raise ServiceError(f"cannot determine ProgramData directory: {result:#x}")
    try:
        return Path(path.value) / _task_name()
    finally:
        ole = ctypes.WinDLL("ole32")
        ole.CoTaskMemFree.argtypes = [wintypes.LPVOID]
        ole.CoTaskMemFree(ctypes.cast(path, wintypes.LPVOID))


def _windows_runtime_state() -> Path:
    return profile.config_dir() / "relayd-service-state"


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


def _privileged(argv: list[str]) -> str:
    if getattr(os, "geteuid", lambda: 1)() == 0:
        return _run(argv)
    return _run(["sudo", "-n", *argv])


def _authorize_macos() -> None:
    global _macos_authorized
    if getattr(os, "geteuid", lambda: 1)() == 0 or _macos_authorized:
        return
    try:
        result = subprocess.run(["sudo", "-v"], check=False)
    except OSError as exc:
        raise ServiceError(f"administrator authorization failed: {exc}") from exc
    if result.returncode:
        raise ServiceError("administrator authorization failed")
    _macos_authorized = True


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
        data = _metadata()
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
        if platform == "win32":
            if data.get("service_dir") != str(_windows_dir()):
                raise ServiceError("the Windows service directory has changed")
            if _windows_state() == "missing":
                raise ServiceError("the registered Windows task is missing")
        return True
    if platform == "linux" and _unit().exists():
        raise ServiceError(
            f"existing service is not managed by this Relay profile: {_unit()}"
        )
    if platform == "darwin" and _plist().exists():
        raise ServiceError(
            f"existing service is not managed by this Relay profile: {_plist()}"
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
    elif _platform() == "win32":
        env["USERPROFILE"] = str(Path.home())
        env["APPDATA"] = str(profile.config_dir().parent.resolve())
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
        "UserName": _owner(),
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


def _windows_check_state() -> None:
    config = profile.config_dir().absolute()
    home = Path.home().absolute()
    if not config.is_relative_to(home):
        raise ServiceError(
            "the SYSTEM service requires the Relay config directory inside the user's profile"
        )
    script = r"""
$ErrorActionPreference = 'Stop'
$ownerHome = [IO.Path]::GetFullPath($env:RELAY_OWNER_HOME).TrimEnd('\')
$config = [IO.Path]::GetFullPath($env:RELAY_CONFIG_DIR).TrimEnd('\')
$owner = [Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$trusted = @($owner, 'S-1-5-18', 'S-1-5-32-544')
$unsafe = [Security.AccessControl.FileSystemRights]::Write -bor
    [Security.AccessControl.FileSystemRights]::Delete -bor
    [Security.AccessControl.FileSystemRights]::DeleteSubdirectoriesAndFiles -bor
    [Security.AccessControl.FileSystemRights]::ChangePermissions -bor
    [Security.AccessControl.FileSystemRights]::TakeOwnership
function Check-Path([string]$path) {
    $attributes = [IO.File]::GetAttributes($path)
    if ($attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw "Reparse points are not allowed in Relay service state: $path"
    }
    if ($attributes -band [IO.FileAttributes]::Directory) {
        $acl = [IO.Directory]::GetAccessControl($path)
    } else {
        $acl = [IO.File]::GetAccessControl($path)
    }
    $ownerSid = $acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
    if ($ownerSid -notin $trusted) { throw "Untrusted owner on $path" }
    $rules = $acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])
    foreach ($rule in $rules) {
        if ($rule.AccessControlType -ne 'Allow') { continue }
        $sid = $rule.IdentityReference.Value
        if ($sid -notin $trusted -and ($rule.FileSystemRights -band $unsafe)) {
            throw "Untrusted account $sid has write access to $path"
        }
    }
    return $attributes
}
$path = $config
while ($true) {
    $null = Check-Path $path
    if ($path -eq $ownerHome) { break }
    $path = [IO.Path]::GetDirectoryName($path)
    if (-not $path) { throw 'Config directory is outside the user profile' }
}
$pending = [Collections.Generic.Stack[string]]::new()
$pending.Push($config)
while ($pending.Count -gt 0) {
    $directory = $pending.Pop()
    foreach ($child in [IO.Directory]::EnumerateFileSystemEntries($directory)) {
        $attributes = Check-Path $child
        if ($attributes -band [IO.FileAttributes]::Directory) { $pending.Push($child) }
    }
}
"""
    _powershell(
        script,
        {"RELAY_OWNER_HOME": str(home), "RELAY_CONFIG_DIR": str(config)},
    )


def _windows_private_dir(path: Path) -> None:
    if sys.platform != "win32":
        raise ServiceError("Windows service registration requires Windows")
    from ctypes import wintypes

    class SecurityAttributes(ctypes.Structure):
        _fields_ = [
            ("nLength", wintypes.DWORD),
            ("lpSecurityDescriptor", wintypes.LPVOID),
            ("bInheritHandle", wintypes.BOOL),
        ]

    descriptor = wintypes.LPVOID()
    advapi = ctypes.WinDLL("advapi32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    convert = advapi.ConvertStringSecurityDescriptorToSecurityDescriptorW
    convert.argtypes = [
        wintypes.LPCWSTR,
        wintypes.DWORD,
        ctypes.POINTER(wintypes.LPVOID),
        wintypes.LPVOID,
    ]
    convert.restype = wintypes.BOOL
    create = kernel.CreateDirectoryW
    create.argtypes = [wintypes.LPCWSTR, ctypes.POINTER(SecurityAttributes)]
    create.restype = wintypes.BOOL
    kernel.LocalFree.argtypes = [wintypes.LPVOID]
    kernel.LocalFree.restype = wintypes.LPVOID
    if not convert(
        "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", 1, ctypes.byref(descriptor), None
    ):
        raise ServiceError(
            f"cannot create service security descriptor: {ctypes.get_last_error()}"
        )
    try:
        attrs = SecurityAttributes(ctypes.sizeof(SecurityAttributes), descriptor, False)
        if not create(str(path), ctypes.byref(attrs)):
            raise ServiceError(
                f"cannot create protected service directory {path}: {ctypes.get_last_error()}"
            )
    finally:
        kernel.LocalFree(descriptor)


def _authorize_windows() -> None:
    if not ctypes.windll.shell32.IsUserAnAdmin():
        raise ServiceError(
            "start --service requires an elevated Administrator terminal on Windows"
        )


WINDOWS_SUPERVISOR = r"""$ErrorActionPreference = 'Stop'
$config = Get-Content -Raw -LiteralPath (Join-Path $PSScriptRoot 'launch.json') | ConvertFrom-Json
[IO.File]::WriteAllText($config.runtimeState, 'starting')
try {
Add-Type -TypeDefinition @'
using System;
using System.Diagnostics;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
public static class RelayJob {
    [StructLayout(LayoutKind.Sequential)]
    struct BasicLimits {
        public long ProcessTime, JobTime;
        public uint Flags;
        public UIntPtr MinWorkingSet, MaxWorkingSet;
        public uint ActiveProcesses;
        public UIntPtr Affinity;
        public uint PriorityClass, SchedulingClass;
    }
    [StructLayout(LayoutKind.Sequential)]
    struct IoCounters { public ulong A, B, C, D, E, F; }
    [StructLayout(LayoutKind.Sequential)]
    struct ExtendedLimits {
        public BasicLimits Basic;
        public IoCounters Io;
        public UIntPtr ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory;
    }
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern IntPtr CreateJobObject(IntPtr attributes, string name);
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern bool SetInformationJobObject(IntPtr job, int type, ref ExtendedLimits info, uint size);
    [DllImport("kernel32.dll", SetLastError=true)]
    static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);
    static IntPtr job;
    public static void Protect() {
        job = CreateJobObject(IntPtr.Zero, null);
        if (job == IntPtr.Zero) throw new System.ComponentModel.Win32Exception();
        ExtendedLimits limits = new ExtendedLimits();
        limits.Basic.Flags = 0x2000;
        if (!SetInformationJobObject(job, 9, ref limits, (uint)Marshal.SizeOf(limits)) ||
            !AssignProcessToJobObject(job, System.Diagnostics.Process.GetCurrentProcess().Handle))
            throw new System.ComponentModel.Win32Exception();
    }
    static string Quote(string value) {
        StringBuilder result = new StringBuilder("\"");
        int slashes = 0;
        foreach (char c in value) {
            if (c == '\\') { slashes++; continue; }
            if (c == '"') {
                result.Append('\\', slashes * 2 + 1);
                result.Append('"');
            } else {
                result.Append('\\', slashes);
                result.Append(c);
            }
            slashes = 0;
        }
        result.Append('\\', slashes * 2);
        return result.Append('"').ToString();
    }
    public static void Run(string binary, string[] arguments, string workingDirectory,
                           string log, string runtimeState) {
        string[] quoted = Array.ConvertAll(arguments, Quote);
        ProcessStartInfo info = new ProcessStartInfo(binary, String.Join(" ", quoted));
        info.WorkingDirectory = workingDirectory;
        info.UseShellExecute = false;
        info.CreateNoWindow = true;
        info.RedirectStandardOutput = true;
        info.RedirectStandardError = true;
        info.StandardOutputEncoding = Encoding.UTF8;
        info.StandardErrorEncoding = Encoding.UTF8;
        object gate = new object();
        DataReceivedEventHandler write = (sender, args) => {
            if (args.Data == null) return;
            lock (gate) {
                File.AppendAllText(log, args.Data + Environment.NewLine);
                if (args.Data.Contains("relayd listening on"))
                    File.WriteAllText(runtimeState, "running");
            }
        };
        using (Process process = new Process()) {
            process.StartInfo = info;
            process.OutputDataReceived += write;
            process.ErrorDataReceived += write;
            process.Start();
            process.BeginOutputReadLine();
            process.BeginErrorReadLine();
            process.WaitForExit();
        }
    }
}
'@
[RelayJob]::Protect()
} catch {
    [IO.File]::AppendAllText($config.log, "relayd supervisor: $($_)`r`n")
    throw
}
foreach ($property in $config.environment.PSObject.Properties) {
    [Environment]::SetEnvironmentVariable($property.Name, [string]$property.Value, 'Process')
}
Set-Location -LiteralPath $config.workingDirectory
$arguments = [string[]]@($config.arguments)
while ($true) {
    try {
        [IO.File]::WriteAllText($config.runtimeState, 'starting')
        [RelayJob]::Run($config.binary, $arguments, $config.workingDirectory,
                        $config.log, $config.runtimeState)
    } catch {
        [IO.File]::AppendAllText($config.log, "relayd supervisor: $($_)`r`n")
    }
    [IO.File]::WriteAllText($config.runtimeState, 'starting')
    Start-Sleep -Seconds 10
}
"""


def _windows_xml(script: Path) -> bytes:
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
    trigger = child(triggers, "BootTrigger")
    child(trigger, "Enabled", "true")
    child(trigger, "ExecutionTimeLimit", "PT0S")
    principals = child(task, "Principals")
    principal = child(principals, "Principal", id="System")
    child(principal, "UserId", "S-1-5-18")
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
    actions = child(task, "Actions", Context="System")
    action = child(actions, "Exec")
    system_root = os.environ.get("SystemRoot", r"C:\Windows")
    child(
        action,
        "Command",
        str(
            Path(system_root)
            / "System32"
            / "WindowsPowerShell"
            / "v1.0"
            / "powershell.exe"
        ),
    )
    child(
        action,
        "Arguments",
        "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "
        + subprocess.list2cmdline([str(script)]),
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
    if platform == "win32":
        _authorize_windows()
        if _windows_state() != "missing":
            raise ServiceError(
                f"existing task is not managed by this Relay profile: {_task_name()}"
            )
        _windows_check_state()
    _prepare_log()
    log_offset = procs.log_path(NAME).stat().st_size
    env = _environment(key)
    metadata_data = {"platform": platform, "argv": argv, "log_offset": log_offset}
    if platform == "win32":
        metadata_data["service_dir"] = str(_windows_dir())
    metadata = json.dumps(metadata_data).encode()
    created = False
    registered = False
    marked = False
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
            _authorize_macos()
            with tempfile.NamedTemporaryFile(
                dir=profile.config_dir(), delete=False
            ) as file:
                temporary = Path(file.name)
                file.write(_macos_plist(argv, env))
            try:
                _privileged(
                    [
                        "/usr/bin/install",
                        "-o",
                        "root",
                        "-g",
                        "wheel",
                        "-m",
                        "600",
                        str(temporary),
                        str(_plist()),
                    ]
                )
            finally:
                temporary.unlink(missing_ok=True)
            created = True
            _write_private(_marker(), metadata)
            marked = True
            _privileged(["/bin/launchctl", "bootstrap", "system", str(_plist())])
            registered = True
        else:
            directory = _windows_dir()
            _windows_private_dir(directory)
            created = True
            binary = directory / "relayd.exe"
            shutil.copyfile(argv[0], binary)
            launch = {
                "binary": str(binary),
                "arguments": argv[1:],
                "workingDirectory": str(profile.config_dir().resolve()),
                "environment": env,
                "log": str(procs.log_path(NAME).resolve()),
                "runtimeState": str(_windows_runtime_state().resolve()),
            }
            _write_private(directory / "launch.json", json.dumps(launch).encode())
            script = directory / "supervisor.ps1"
            _write_private(script, WINDOWS_SUPERVISOR.encode())
            xml = directory / "task.xml"
            _write_private(xml, _windows_xml(script))
            _write_private(_marker(), metadata)
            marked = True
            _run(["schtasks.exe", "/Create", "/TN", _task_name(), "/XML", str(xml)])
            registered = True
            _run(["schtasks.exe", "/Run", "/TN", _task_name()])
    except (OSError, ServiceError) as exc:
        failures = []
        if registered:
            try:
                if platform == "linux":
                    _run(["systemctl", "--user", "disable", "--now", "relayd.service"])
                elif platform == "darwin":
                    _privileged(["/bin/launchctl", "bootout", "system", str(_plist())])
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
                    _privileged(["/bin/rm", "-f", str(_plist())])
                else:
                    shutil.rmtree(_windows_dir())
            except (OSError, ServiceError) as rollback:
                failures.append(str(rollback))
        if marked and not failures:
            _marker().unlink(missing_ok=True)
        detail = f"; cleanup failed: {'; '.join(failures)}" if failures else ""
        raise ServiceError(f"could not install service: {exc}{detail}") from exc


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
        _authorize_macos()
        try:
            output = _privileged(["/bin/launchctl", "print", f"system/{LABEL}"])
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
        try:
            runtime = _windows_runtime_state().read_text(encoding="utf-8").strip()
        except FileNotFoundError:
            return "activating"
        except OSError as exc:
            raise ServiceError(f"cannot read Windows service state: {exc}") from exc
        if runtime == "running":
            return "active"
        if runtime in {"", "starting"}:
            return "activating"
        raise ServiceError("invalid Windows service runtime state")
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
        _authorize_macos()
        _privileged(["/bin/launchctl", "bootstrap", "system", str(_plist())])
    else:
        _run(["schtasks.exe", "/Run", "/TN", _task_name()])


def stop() -> bool:
    if state() == "inactive":
        return False
    platform = _platform()
    if platform == "linux":
        _run(["systemctl", "--user", "stop", "relayd.service"])
    elif platform == "darwin":
        _authorize_macos()
        _privileged(["/bin/launchctl", "bootout", "system", str(_plist())])
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
            _authorize_macos()
            _privileged(["/bin/rm", str(_plist())])
        else:
            stop()
            _run(["schtasks.exe", "/Delete", "/TN", _task_name(), "/F"])
            shutil.rmtree(_windows_dir())
            _windows_runtime_state().unlink(missing_ok=True)
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
