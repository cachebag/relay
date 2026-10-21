# Setup

To develop `relay`, you only need two tools on your device.

## 1. Install Go

**Windows** — download the installer from [https://go.dev/dl/](https://go.dev/dl/) and run it. Restart your terminal after the installation finishes.

**macOS** — `brew install go`, or the installer from [https://go.dev/dl/](https://go.dev/dl/).

**Linux** — `sudo pacman -S go` / `sudo apt install golang-go`, or the tarball from [https://go.dev/dl/](https://go.dev/dl/).

Check:

```
go version
```

Should say 1.27 or newer.

## 2. Install uv

**Windows** (PowerShell):

```
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

**macOS / Linux**:

```
curl -LsSf https://astral.sh/uv/install.sh | sh
```

Restart your terminal. Check:

```
uv --version
```

## 3. Clone this repo

```
git clone https://github.com/cachebag/relay.git
cd relay
```

## 4. Go — build and run

```
go build ./...
go run ./cmd/relayd
```

Should print `hello, relay`.

## 5. Python CLI — install and run

```
cd cli
uv sync
uv run relay
```

`uv sync` downloads Python 3.14 if you don't have it and creates a virtualenv in `cli/.venv`.

Should print `hello, relay`.

## 6. Scheduler address

By default, the Relay CLI and agent connect to the scheduler at:

```
http://localhost:8080
```

You can change the default scheduler address with the `RELAY_SCHEDULER` environment variable.

**Windows PowerShell:**

```
$env:RELAY_SCHEDULER="http://localhost:9090"
```

**macOS / Linux:**

```
export RELAY_SCHEDULER="http://localhost:9090"
```

The `--scheduler` command-line flag takes precedence over `RELAY_SCHEDULER`.

For example:

```
relay --scheduler http://localhost:9999 nodes
```

The scheduler address is selected in this order:

1. `--scheduler`
2. `RELAY_SCHEDULER`
3. `http://localhost:8080`

### Public address

`relayd --public` also serves on a public `https://` address through Tailscale Funnel, for contributors who aren't on your network. It needs `TS_AUTHKEY` set to a reusable auth key from your Tailscale admin console, and exits with code 2 if it isn't. `--name` picks the hostname (default `relay`). See [TAILSCALE.md](TAILSCALE.md) for the one-time Tailscale setup with screenshots. You don't need this for local development; the CLI and agent on your own machine use `http://localhost:8080`.

## 7. Tests

Go:

```
go test ./...
```

Python (from `cli/`):

```
uv run pytest
```

Both will say "no tests" right now. That's expected.

## Resetting your local database

`relayd` stores nodes, pools, and jobs in `relay.db` in whatever directory you run it from. To wipe it and start clean, stop `relayd` and run from the repo root:

```
go run ./tools/resetdb
```

It asks for confirmation, then deletes `relay.db` and its `-wal`/`-shm` files. Flags: `-db <path>` for a different database, `-y` to skip the prompt, `-port <n>` if `relayd` isn't on 8080, and `-force` to skip the "is relayd still running" check.

This is a dev-only tool. It is not part of any release binary or the `relay` CLI.

## Where code goes

| Path | What |
|---|---|
| `cmd/relayd` | control plane binary |
| `cmd/relay-agent` | node agent binary |
| `tools/*` | dev-only helpers, never released |
| `internal/*` | Go packages, one per concern |
| `cli/relay/` | Python CLI |
| `docs/adr/` | design decisions |

## Windows notes

- Use **Git Bash** (comes with Git for Windows) if a command in the docs uses bash syntax. PowerShell works for everything above.
- The node agent's isolation code is Linux-only. If you're on the agent track, you'll need WSL2 or SSH to a Linux machine — ask Akrm on how to set this up.