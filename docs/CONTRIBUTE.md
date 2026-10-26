# Contribute a machine

You need the scheduler's address and a pool token from your pool admin. Contributing needs nothing but the agent binary.

Release assets are named `relay-agent-<os>-<arch>` (`.exe` on Windows), where `<os>-<arch>` is one of `linux-amd64`, `darwin-arm64`, `darwin-amd64`, `windows-amd64`. The examples below use `linux-amd64`; swap in yours.

The pool token can be provided with the `--pool-token` flag or through the
`RELAY_POOL_TOKEN` environment variable. If both are provided, `--pool-token`
takes precedence.

Your machine earns 1 credit for every minute it's online. The credits go to
the name given with `--owner` or `RELAY_OWNER`, or your username if neither is
set. Run `relay credits` to see the balances in your pool.

## With the CLI

If you have the `relay` CLI (see [SUBMIT.md](SUBMIT.md)), it downloads the right agent for your machine:

```sh
relay install --agent-only
```

It goes into `relay/bin` under your config folder and the command prints the path. Then run it with the address and token from your admin, as below.

## Linux and macOS

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

## Windows

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

## Scheduler address

`relay-agent` finds the scheduler the same way the CLI does: the `--scheduler` flag
if you pass it, otherwise the `RELAY_SCHEDULER` environment variable, otherwise
`http://localhost:8080`. Setting the variable once saves typing `--scheduler` every time:

```sh
export RELAY_SCHEDULER="http://<scheduler address>:8080"    # Linux and macOS
```

```powershell
$env:RELAY_SCHEDULER="http://<scheduler address>:8080"     # Windows PowerShell
```

## Leave a pool

Stop the agent first (Ctrl-C), then run `leave` with the same scheduler and token:

```sh
./relay-agent-linux-amd64 leave --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

The machine disappears from `relay nodes` right away, any job it was running goes to another machine, and the agent deletes the files it kept on this machine. If the agent is still running, its next heartbeat joins the pool again.
