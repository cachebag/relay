"""The saved logins: scheduler, pool, join token, and user name for each pool.

This is the only file the CLI keeps outside its install. It holds join
tokens, so it is written readable by the owner only on Linux and macOS.

You can be logged in to several pools. One of them is current, and that is
what every command uses. `relay login` adds a pool, `relay use` switches,
`relay logout` forgets the current one.
"""

import json
import os
import sys
from pathlib import Path

FILENAME = "cli.json"


def config_dir() -> Path:
    if sys.platform == "linux":
        base = os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config"
        return Path(base) / "relay"
    if sys.platform == "darwin":
        return Path.home() / "Library" / "Application Support" / "relay"
    if sys.platform == "win32":
        base = os.environ.get("APPDATA") or Path.home() / "AppData" / "Roaming"
        return Path(base) / "relay"
    # Error message for unsupported platforms
    raise RuntimeError(f"Unsupported platform: {sys.platform}")


def path() -> Path:
    return config_dir() / FILENAME


def _key(login: dict) -> tuple:
    return (login.get("scheduler"), login.get("pool_id"))


def _read() -> dict:
    """The file as {"current": <pool_id>, "logins": [...]}.

    Files written before multi-pool support hold one login as a flat dict,
    and are read as a list of one."""
    try:
        data = json.loads(path().read_text())
    except OSError, ValueError:
        return {"current": None, "logins": []}
    if not isinstance(data, dict):
        return {"current": None, "logins": []}
    if "logins" not in data:
        return (
            {"current": data.get("pool_id"), "logins": [data]}
            if data
            else {"current": None, "logins": []}
        )
    return data


def _write(data: dict) -> None:
    p = path()
    p.parent.mkdir(parents=True, exist_ok=True)
    # Create or overwrite the profile file with owner only rw permissions
    fd = os.open(p, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(data, f, indent=2)
    if sys.platform != "win32":
        p.chmod(0o600)


def server() -> dict:
    """Settings for the scheduler this machine runs, if any: admin_token,
    address, public_url, ts_authkey. Empty if relay scheduler start never ran."""
    data = _read()
    srv = data.get("server")
    return srv if isinstance(srv, dict) else {}


def save_server(**fields: str) -> dict:
    """Merge fields into the server settings and return them."""
    data = _read()
    srv = data.get("server") if isinstance(data.get("server"), dict) else {}
    srv.update({k: v for k, v in fields.items() if v is not None})
    data["server"] = srv
    _write(data)
    return srv


def logins() -> list[dict]:
    """Every saved login, in the order they were added."""
    return [login for login in _read()["logins"] if isinstance(login, dict)]


def load() -> dict:
    """The current login, or an empty dict if not logged in anywhere."""
    data = _read()
    for login in data["logins"]:
        if isinstance(login, dict) and login.get("pool_id") == data["current"]:
            return login
    return data["logins"][0] if data["logins"] else {}


def save(profile: dict) -> None:
    """Add a login, or replace the saved one for the same pool, and make it current."""
    data = _read()
    others = [login for login in data["logins"] if _key(login) != _key(profile)]
    data["logins"] = others + [profile]
    data["current"] = profile.get("pool_id")
    _write(data)


def use(pool: str) -> dict:
    """Make the login for this pool ID or name current. Raises KeyError if unknown."""
    data = _read()
    for field in ("pool_id", "pool_name"):
        for login in data["logins"]:
            if login.get(field) == pool:
                data["current"] = login.get("pool_id")
                _write(data)
                return login
    raise KeyError(pool)


def delete() -> dict:
    """Forget the current login. Returns the login now current, or {} if none."""
    data = _read()
    current = load()
    remaining = [login for login in data["logins"] if _key(login) != _key(current)]
    if not remaining:
        if data.get("server"):
            data["logins"], data["current"] = [], None
            _write(data)
        else:
            path().unlink(missing_ok=True)
        return {}
    data["logins"] = remaining
    data["current"] = remaining[0].get("pool_id")
    _write(data)
    return remaining[0]


def require_login() -> dict:
    """Return the saved profile, or exit 1 with a hint if there isn't one.

    Every command that talks to a pool needs the join token from the login,
    so they all go through here.
    """
    prof = load()
    if not prof.get("pool_token"):
        print("not logged in, run relay login first", file=sys.stderr)
        sys.exit(1)
    return prof


def bearer(token: str) -> dict:
    """The Authorization header the scheduler expects."""
    return {"Authorization": f"Bearer {token}"}
