
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

Your machine earns 1 credit for every minute it's online. The credits go to
the name given with `--owner` or `RELAY_OWNER`, or your username if neither is
set. Run `relay credits` to see the balances in your pool.

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

### Leave a pool

Stop the agent first (Ctrl-C), then run `leave` with the same scheduler and token:

```sh
./relay-agent-linux-amd64 leave --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

The machine disappears from `relay nodes` right away, any job it was running goes to another machine, and the agent deletes the files it kept on this machine. If the agent is still running, its next heartbeat joins the pool again.

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
```

The first time it starts, `relayd` prints a server admin token. Save it. It is the only thing that can create pools on this scheduler, so strangers can't make pools on your server once it is reachable from the internet. If you lose it, `./relayd-linux-amd64 --new-admin-token` prints a new one and exits.

```sh
# in another terminal, with the CLI installed
relay pool create my-pool --admin-token <server admin token>
```

This prints the pool's join token, which you send to contributors and submitters, and the pool's admin token, which only you keep. Instead of `--admin-token` you can set `RELAY_ADMIN_TOKEN`.

If you create a pool using a scheduler on your own machine (for example,
`http://localhost:8080`), Relay prints an invitation like this:

```text
relay-agent --scheduler http://<your address>:8080 --pool-token <join token>
replace <your address> with an address contributors can reach, for example your Tailscale name.
```

Before sharing the command, replace `<your address>` with the scheduler's reachable
LAN IP address or Tailscale hostname. The placeholder is not a usable address,
and `localhost` on a contributor's computer refers to their own computer, not
the admin's scheduler. Relay keeps the original port in the invitation. If you
already connect using a non-local scheduler address, the command displays that
address unchanged without the replacement hint.

`relay pool rotate-token` prints the same contributor command and address hint
when applicable. After rotating, distribute the new join token and restart
contributor agents, since the old join token no longer works. Never share the
server or pool admin token with contributors.

Every other command works on the pool you are logged in to (`relay login`), and sends its join token with each request. Nobody without a pool's join token can see its machines, jobs, or output.


## Submitter: Run a job

With the CLI installed, log in using your pool token:

```sh
relay login --scheduler http://<scheduler>:8080 --pool-token <pool-token> --name <your-name>
```

Submit a job and wait for it to finish:

```sh
relay submit --wait -- uname -n
```

The CLI checks the job's status once per second, prints each status change, then displays the job's output and exits with the job's exit code.

To stop waiting after a certain number of seconds, use `--timeout`:

```sh
relay submit --wait --timeout 30 -- uname -n
```

If the timeout expires, the CLI exits with code `124`. Pressing Ctrl+C while waiting exits with code `130`. In both cases, the submitted job continues running and can be checked later.

To submit a job without waiting, omit `--wait`:

```sh
relay submit -- uname -n
relay status <id>
relay logs <id>
```


# For developers

Please read the [setup guide](docs/SETUP.md) to get your development environment setup.

## Stack

* Go (scheduler and node agent)
* Python (CLI)
* Tailscale (networking)
* SQLite (job and node state)
