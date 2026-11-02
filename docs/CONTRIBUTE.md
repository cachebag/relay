# Contribute a machine

You need the scheduler's address and a pool token from your pool admin. Contributing needs nothing but the agent binary.

Release assets are named `relay-agent-<os>-<arch>.tar.gz` (`.exe` on Windows), where `<os>-<arch>` is one of `linux-amd64`, `darwin-arm64`, `darwin-amd64`, `windows-amd64`. The examples below use `linux-amd64`; swap in yours.

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

It goes into `relay/bin` under your config folder and puts that folder on your PATH. Then log in once with the address and token from your admin, and start contributing:

```sh
relay login --scheduler http://<scheduler address>:8080 --pool-token <pool token> --name <your name>
relay contribute
```

`relay contribute` runs the agent in the background for the pool you are logged in to, with your saved token and name. The terminal is free again and the agent keeps going. If it dies in its first ten seconds, the command says so and shows the last line of its log.

```sh
relay contribute status   # which pool, and the agent's process ID
relay contribute stop     # stops the agent; its current job goes back to the queue
```

In the dashboard, `a` does the same start and stop, and the top bar shows `this machine: contributing` while it runs. The agent's output is in `relay-agent.log` in your relay config folder. `stop` only knows about agents started this way; one you ran by hand in a terminal is stopped with Ctrl+C there.

To run the agent by hand instead, use the commands below.

## Linux and macOS

```sh
curl -L https://github.com/cachebag/relay/releases/latest/download/relay-agent-linux-amd64.tar.gz | tar xz
./relay-agent --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

Alternatively, set the pool token as an environment variable:

```sh
export RELAY_POOL_TOKEN="<pool token>"
./relay-agent --scheduler http://<scheduler address>:8080
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

## Where jobs run

Each job runs in its own empty folder, `relay/jobs/<job id>` in your cache folder (`~/.cache` on Linux, `~/Library/Caches` on macOS, `%LocalAppData%` on Windows), with `HOME` and the temp folder pointed inside it. The folder is deleted once the result is reported, and any left behind by a crash are deleted when the agent next starts. Jobs still run as your user, so this keeps their files tidy but doesn't stop a job reading yours.

To look at what a job left behind, start the agent with `--keep-workdir`. It keeps every job's folder and logs where it is.

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
./relay-agent leave --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

The machine disappears from `relay nodes` right away, any job it was running goes to another machine, and the agent deletes the files it kept on this machine. If the agent is still running, its next heartbeat joins the pool again.
