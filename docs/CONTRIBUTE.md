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

`relay contribute` runs an agent in the background for the **current** saved
pool, using its token, scheduler, and your saved name. The terminal is free
again while the agent keeps running. Use `relay pools` to list saved logins or
`relay use <pool name or ID>` to change the current pool.

Each pool has its **own agent process**. Starting a second pool does not stop
the first, and starting the same pool twice reports that it is already
contributing. To choose a saved pool without changing your current login:

```sh
relay contribute --pool <pool name or ID>
```

If an agent exits during its first ten seconds, the command reports the last
line of its log. The scheduler can still refuse contributions to two pools on
the **same scheduler**; the CLI displays that error rather than overriding the
scheduler's restriction.

### Inspect and stop agents

```sh
relay contribute status                   # all running pools, PIDs, and schedulers
relay contribute stop                     # stop only the current pool's agent
relay contribute stop --pool <pool name>  # stop only the named pool's agent
relay contribute stop --all               # stop all locally tracked agents
```

Stopping an agent returns its current job to the queue. `status` also lists
agents whose saved login was removed after they started, and `stop --all` can
stop those processes. A manually launched agent is **not** managed by these
commands; stop it with Ctrl+C in its own terminal.

In `relay dash`, the **a** key starts or stops only the pool currently shown
in the dashboard. Switching pools leaves other agents running. The top bar
shows `this machine: contributing` for one active current pool, or
`this machine: contributing to 2 pools` when two are running. It also indicates
when another pool is running but the selected pool is not.

Each CLI-managed agent writes separate tracking files in the Relay config
folder (`~/.config/relay` on Linux, `~/Library/Application Support/relay` on
macOS, `%AppData%\relay` on Windows):

| File | Purpose |
| --- | --- |
| `relay-agent.<pool-id>.pid` | Process ID used for status and stopping |
| `relay-agent.<pool-id>.state.json` | Pool and scheduler metadata |
| `relay-agent.<pool-id>.log` | Agent output and startup errors |

An agent started by an older CLI using `relay-agent.pid` is still recognized
until it stops, so upgrading does not silently orphan a running process.

### Verify contributions to two schedulers

Log in to a pool on each of two **different** schedulers, using their own
join tokens. Each `relay login` saves the pool and makes it current:

```sh
relay login --scheduler http://scheduler-a.example:8080 --pool-token <token-a> --name <your name>
relay login --scheduler http://scheduler-b.example:8080 --pool-token <token-b> --name <your name>
relay pools
```

Use the pool names or IDs shown by `relay pools` for the following commands:

```sh
relay contribute --pool pool-a
relay contribute --pool pool-b
relay contribute status
relay contribute stop --pool pool-a
relay contribute status
relay contribute stop --all
```

After starting both, `status` should show **two different PIDs** and both
scheduler addresses, and the machine should appear online in both dashboards.
Stopping pool A should leave pool B online and listed in `status`. The final
`stop --all` should leave no CLI-managed agents running. You can also switch
between the pools in `relay dash` and verify that **a** controls only the pool
being viewed.

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
