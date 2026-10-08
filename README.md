<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://github.com/user-attachments/assets/c4043fe3-7570-4408-a93e-7644c74e6a86">
    <img alt="relay" src="https://github.com/user-attachments/assets/91f7d398-c90b-474b-8c20-1a2d643ebb73" width="360">
  </picture>
</p>

Turn idle laptops into a shared compute pool. Submit jobs from
anywhere, run them macOS or Linux.

The idea of `relay` is that you can volunteer a machine you aren't
using and it becomes a node. Anyone in the pool can submit jobs to it.

Contributing also earns credits; submitting spends them, and the more
points someone has, the higher priority their jobs get.


# Quickstart

Release assets are named `relayd-<os>-<arch>` and `relay-agent-<os>-<arch>` (`.exe` on Windows),
where `<os>-<arch>` is one of `linux-amd64`, `darwin-arm64`, `darwin-amd64`, `windows-amd64`.
The examples below use `linux-amd64`; swap in yours.

The `relay` CLI (for admins and submitters) needs [uv](https://docs.astral.sh/uv/getting-started/installation/).
Contributing a machine needs nothing but the agent binary.

## Scheduler address

`relay` and `relay-agent` both find the scheduler the same way: the `--scheduler` flag
if you pass it, otherwise the `RELAY_SCHEDULER` environment variable, otherwise
`http://localhost:8080`. Setting the variable once saves typing `--scheduler` on every command:

```sh
export RELAY_SCHEDULER="http://<scheduler address>:8080"    # Linux and macOS
```

```powershell
$env:RELAY_SCHEDULER="http://<scheduler address>:8080"     # Windows PowerShell
```

## Contribute a machine

You need the scheduler's address and a pool token from your pool admin.

The pool token can be provided with the `--pool-token` flag or through the
`RELAY_POOL_TOKEN` environment variable. If both are provided, `--pool-token`
takes precedence.

### Linux and macOS

```sh
curl -LO https://github.com/cachebag/relay/releases/latest/download/relay-agent-linux-amd64
chmod +x relay-agent-linux-amd64
./relay-agent-linux-amd64 --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

Alternatively, set the pool token as an environment variable:

```sh
export RELAY_POOL_TOKEN="<pool token>"
./relay-agent-linux-amd64 --scheduler http://<scheduler address>:8080
```

> [!NOTE]
> If you download the binary with a browser instead of `curl`, macOS will refuse to run it
> until you right-click → Open once.

### Windows

From PowerShell:

```powershell
curl.exe -LO https://github.com/cachebag/relay/releases/latest/download/relay-agent-windows-amd64.exe
.\relay-agent-windows-amd64.exe --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

Alternatively, set the pool token as an environment variable:

```powershell
$env:RELAY_POOL_TOKEN="<pool token>"
.\relay-agent-windows-amd64.exe --scheduler http://<scheduler address>:8080
```

> Use `curl.exe`, not `curl`: in Windows PowerShell, `curl` is an alias for a different command.

## Install the CLI

```sh
uv tool install "git+https://github.com/cachebag/relay#subdirectory=cli"
```

After install, `relay` works from any directory. Update or remove it with:

```sh
uv tool upgrade relay
uv tool uninstall relay
```

## Admin: Run a scheduler and create a pool

```sh
curl -LO https://github.com/cachebag/relay/releases/latest/download/relayd-linux-amd64
chmod +x relayd-linux-amd64
./relayd-linux-amd64                  # listens on :8080; leave it running

# in another terminal, with the CLI installed
relay pool create my-pool             # prints a join token; send it to contributors
```

## Submitter: Run a job

With the CLI installed:

```sh
relay --scheduler http://<scheduler>:8080 submit -- echo hi   # prints <id>
relay --scheduler http://<scheduler>:8080 status <id>
relay --scheduler http://<scheduler>:8080 logs <id>
```

# For developers

Please read the [setup guide](docs/SETUP.md) to get your development environment setup.

## Stack

* Go (scheduler and node agent)
* Python (CLI)
* Tailscale (networking)
* SQLite (job and node state)