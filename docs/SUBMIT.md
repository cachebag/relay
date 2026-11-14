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

To stop waiting after a certain number of seconds, use `--wait-timeout`:

```sh
relay submit --wait --wait-timeout 30 -- uname -n
```

If the timeout expires, the CLI exits with code `124`. Pressing Ctrl+C while waiting exits with code `130`. In both cases, the submitted job continues running and can be checked later.

### Job execution timeouts

Jobs have a one-hour execution limit by default, up to a maximum of 24 hours.
Set a different limit with `--timeout`:

```sh
relay submit --timeout 10m -- uname -n
```

Jobs that exceed their limit are killed and marked `failed`. The timeout
message (for example, `relay: job timed out after 10m`) appears in `relay logs`.

## Send files with a job

A job can carry its own files, so the machine that runs it doesn't need anything installed up front except the interpreter. Put what the job needs in a folder with a `run.sh` (Linux and macOS) or `run.ps1` or `run.cmd` (Windows) at the top, and submit the folder:

```text
crunch/
  run.sh
  crunch.py
  data.csv
```

```sh
# run.sh
python3 crunch.py data.csv "$@"
```

```python
# crunch.py: add up the numbers in a CSV, scaled by the first argument
import csv, os, sys

rows = list(csv.reader(open(sys.argv[1])))
total = sum(int(x) for row in rows for x in row) * int(sys.argv[2])
os.makedirs("out", exist_ok=True)
with open("out/result.csv", "w") as f:
    f.write(f"total,{total}\n")
print("wrote out/result.csv")
```

```sh
relay submit --wait --dir ./crunch -- 10
```

The folder is packed, sent with the job, and unpacked into the job's own empty folder on the node. `run.sh` runs there with the arguments after `--`. Anything the job writes to `out/` comes back with the result: `relay status` and the dashboard list the files, and `relay fetch` downloads them. Folders are capped at 50 MB packed, and `.git`, `__pycache__`, `.venv` and `node_modules` are left out.

Once the job has finished, get its files with the job id:

```sh
relay fetch 40ca4109f082
```

They land in `relay-out/<job id>/` in the current folder, or in the folder named with `--to`. Files that are already there are left alone unless you add `--force`. In the dashboard, open the job with enter and press `f`.

For one script, `--file` sends just that file and runs it:

```sh
relay submit --wait --file crunch.py -- data.csv 10
```

To run something other than `run.*`, name it with `--entry`:

```sh
relay submit --dir ./crunch --entry crunch.py -- data.csv 10
```

The node picks the interpreter from the file's extension: `.sh` with `sh`, `.py` with `python3` (`python` on Windows), `.ps1` with PowerShell, `.cmd` with `cmd`, and anything else is run as a program. Add `--os linux` if the job only works on one operating system.

In `relay dash`, `j` does the same without the flags: type the folder or script, and it shows how many files will go and what will run before you send it. Arguments, entry, OS and time limit are fields on the same screen, and it remembers them for the next job.

## See what's in the pool

```sh
relay nodes       # machines and whether they're online
relay jobs        # every job, who submitted it, how long it ran
relay credits     # credit balances in your pool
relay dash        # live dashboard
```
