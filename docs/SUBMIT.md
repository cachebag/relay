# Install the CLI and run a job

The `relay` CLI needs [uv](https://docs.astral.sh/uv/getting-started/installation/).

## Install

```sh
uv tool install "git+https://github.com/cachebag/relay#subdirectory=cli"
```

After install, `relay` works from any directory. Update or remove it with:

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

## See what's in the pool

```sh
relay nodes       # machines and whether they're online
relay jobs        # every job, who submitted it, how long it ran
relay credits     # credit balances in your pool
relay dash        # live dashboard
```
