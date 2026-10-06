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

You need [uv](https://docs.astral.sh/uv/getting-started/installation/) and the
[GitHub CLI](https://cli.github.com/).

Release assets are named `relayd-<os>-<arch>`, `relay-agent-<os>-<arch>` (`.exe` on Windows)
and `relay-<version>-py3-none-any.whl`, where `<os>-<arch>` is one of `linux-amd64`,
`darwin-arm64`, `darwin-amd64`, `windows-amd64`.

Without the GitHub CLI, you can download any asset from the latest release with `curl`:

```sh
curl -LO https://github.com/cachebag/relay/releases/latest/download/<asset>
```

> [!WARNING]
> The `curl` download won't work until the repo is public. Until then, use `gh release download`.

## Scheduler address

`relay` and `relay-agent` both find the scheduler the same way: the `--scheduler` flag
if you pass it, otherwise the `RELAY_SCHEDULER` environment variable, otherwise
`http://localhost:8080`. Setting the variable once saves typing `--scheduler` on every command:

```sh
export RELAY_SCHEDULER="http://<scheduler address>:8080"    # Linux and macOS
```

```powershell
$env:RELAY_SCHEDULER="http://<scheduler address>:8080"      # Windows PowerShell
```

## Contribute a machine  

First download the binary for your OS from the latest release: 

```sh
gh release download --repo cachebag/relay --pattern 'relay-agent-linux-amd64'
```

> Replace `relay-agent-linux-amd64` with the asset for your OS and architecture (see the list above).

  
### Linux and macOS:  

Then, run:  

```sh
chmod +x relay-agent-linux-amd64
./relay-agent-linux-amd64 --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```
> Replace `<scheduler address>`, and `<pool token>` with the scheduler's ipv4 address, and your pool token.

>[!NOTE]
>macOS will refuse to run an unsigned download until you right-click → Open once.

  
### Windows:  

Then, run from PowerShell:

```powershell
./relay-agent-windows-amd64.exe --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```
> Replace `<scheduler address>`, and `<pool token>` with the scheduler's ipv4 address, and your pool token.


## Admin: Run a scheduler and create a pool

```sh
gh release download --repo cachebag/relay --pattern 'relayd-linux-amd64'
chmod +x relayd-linux-amd64
./relayd-linux-amd64                  # listens on :8080; leave it running

# in another terminal
gh release download --repo cachebag/relay --pattern '*.whl'
uv tool install ./relay-*.whl
relay pool create my-pool             # prints a join token; send it to contributors
```

## Submitter: Run a job

```sh
gh release download --repo cachebag/relay --pattern '*.whl'
uv tool install ./relay-*.whl
relay --scheduler http://<scheduler>:8080 submit -- echo hi   # prints <id>
relay --scheduler http://<scheduler>:8080 status <id>
relay --scheduler http://<scheduler>:8080 logs <id>
```

After install, `relay` works from any directory. Update or remove it with:

> Delete any older `relay-*.whl` in the folder before downloading a new one,
> otherwise `./relay-*.whl` matches more than one file and the install fails.

```sh
uv tool install --force ./relay-*.whl   # upgrade: download the newer wheel, then run this
uv tool upgrade relay                   # only picks up changes for indexed installs, not local wheels
uv tool uninstall relay
```

# For developers

Please read the [setup guide](docs/SETUP.md) to get your development environment setup.

## Stack

* Go (scheduler and node agent)
* Python (CLI)
* Tailscale (networking)
* SQLite (job and node state)