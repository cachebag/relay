# Install the CLI and run a job

The `relay` CLI needs [uv](https://docs.astral.sh/uv/getting-started/installation/).

## Install

```sh
uv tool install "git+https://github.com/cachebag/relay#subdirectory=cli"
```

After install, `relay` works from any directory. `relay install` then downloads `relayd` and `relay-agent` for this machine if you need them (`--agent-only` for just the agent, `--version v0.2.5` to pin one). Update or remove the CLI with:

```sh
uv tool upgrade relay
uv tool uninstall relay
```

## Scheduler address

`relay` finds the scheduler the same way the agent does: the `--scheduler` flag
if you pass it, otherwise the `RELAY_SCHEDULER` environment variable, otherwise
`http://localhost:8080`. Once you log in, the saved login is used too, so you
usually only pass `--scheduler` once.

```sh
export RELAY_SCHEDULER="http://<scheduler address>:8080"    # Linux and macOS
```

```powershell
$env:RELAY_SCHEDULER="http://<scheduler address>:8080"     # Windows PowerShell
```

## Log in

Log in using the pool token from your admin:

```sh
relay login --scheduler http://<scheduler>:8080 --pool-token <pool-token> --name <your-name>
```

Every command after that works on the pool you are logged in to. `relay whoami` shows the saved login and `relay logout` forgets it.

You can be logged in to more than one pool. `relay login` with another pool's token adds it, `relay pools` lists them, and `relay use <name>` switches. `relay logout` drops the current pool and moves you to the next one. In the dashboard, `p` does the same.

## Run a job

Submit a job and wait for it to finish:

```sh
relay submit --wait -- uname -n
```

The CLI checks the job's status once per second, prints each status change, then displays the job's output and exits with the job's exit code.

### Job execution timeouts

Every job has a time limit of **1 hour (3,600 seconds) by default**. This prevents long-running or unresponsive jobs from occupying an agent indefinitely.

Use `--timeout` to specify a different execution limit:

```sh
relay submit --timeout 5s -- uname -n
relay submit --timeout 10m -- uname -n
relay submit --timeout 1h30m -- uname -n
```

The flag accepts Go-style durations:

- `s` - seconds
- `m` - minutes
- `h` - hours
- `ms` - milliseconds
- `us` - microseconds
- `ns` - nanoseconds

Combined durations are also supported, such as `1h30m` or `2m15s`.

Subsecond durations are rounded up to the nearest whole second because the API stores timeouts as integer seconds.

Plain seconds are accepted as well:

```sh
relay submit --timeout 600 -- uname -n
```

This is equivalent to:

```sh
relay submit --timeout 10m -- uname -n
```

Valid execution limits range from **1 second to 24 hours (86,400 seconds)**.

The following values are rejected:

```sh
relay submit --timeout 0 -- uname -n
relay submit --timeout -5m -- uname -n
relay submit --timeout 25h -- uname -n
```

When a job exceeds its configured execution limit, the agent terminates the running process and its child processes.

The job is then:

- Marked as `failed`.
- Assigned an exit code of `-1`.
- Given a timeout explanation in its error output.
- Removed from the agent's active workload so another job can run.

For example, a job with a five-second limit will report:

```text
relay: job timed out after 5s
```

The timeout is applied to each execution attempt independently.

If `--timeout` is omitted, the API automatically assigns the default one-hour limit.

### Waiting for job completion

The `--wait` flag tells the CLI to monitor the submitted job until it finishes.

```sh
relay submit --wait -- uname -n
```

To stop waiting locally after a certain number of seconds, use `--wait-timeout`:

```sh
relay submit --wait --wait-timeout 30 -- uname -n
```

Unlike `--timeout`, the `--wait-timeout` option does **not** terminate the job.

If the local waiting limit expires, the CLI exits with code `124`.

Pressing Ctrl+C while waiting exits with code `130`.

In both cases, the submitted job continues running on the agent, subject to its separate execution limit, and can be checked later.

You can also combine the two timeout options:

```sh
relay submit --timeout 10m --wait --wait-timeout 30 -- uname -n
```

This command:

1. Submits a job with a maximum execution time of 10 minutes.
2. Waits locally for up to 30 seconds.
3. Stops waiting if the job has not finished within 30 seconds.
4. Allows the remote job to continue running until it finishes or reaches its execution limit.

### Submit without waiting

To submit a job without waiting, omit `--wait`:

```sh
relay submit -- uname -n
```

The CLI immediately returns the job ID and its initial status.

You can check the job later:

```sh
relay status <id>
relay logs <id>
```

You can also submit a job with a custom execution limit without waiting:

```sh
relay submit --timeout 5m -- uname -n
```

The configured time limit is stored with the job and remains available after the scheduler restarts.

## See what's in the pool

```sh
relay nodes       # machines and whether they're online
relay jobs        # every job, who submitted it, how long it ran
relay credits     # credit balances in your pool
relay dash        # live dashboard
```