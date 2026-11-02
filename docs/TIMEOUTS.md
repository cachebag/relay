# Job time limits

Every Relay job has a time limit for **each execution attempt**. The default is
**1 hour (3600 seconds)**, and the maximum is **24 hours (86400 seconds)**.
Time spent queued or waiting for an available node does not count against the
execution limit.

## Choose a limit

Use `--timeout` when submitting a job. The CLI accepts plain seconds or
Go-style durations such as `5s`, `10m`, `1h`, and `1h30m`:

```sh
relay submit --timeout 5s -- sleep 60
relay submit --timeout 600 -- sleep 60
relay submit --timeout 1h30m -- sleep 60
```

The `sleep` examples above are for macOS and Linux. For a long-running Windows
job, use `ping 127.0.0.1 -n 60` as shown in the Windows test below.

If `--timeout` is omitted, the scheduler records `timeout_seconds: 3600`.
The `POST /jobs` API accepts an optional `timeout_seconds` integer from `1`
through `86400`; zero, negative values, and values above the maximum return
`400 Bad Request`. The field is also included in `GET /jobs` and
`GET /jobs/{id}` responses.

`--wait-timeout` is different. It limits how long **your CLI waits** with
`--wait`, without cancelling or shortening the job's execution deadline:

```sh
relay submit --timeout 10m --wait --wait-timeout 30 -- sleep 60
```

## When a job times out

The agent runs each command with a deadline. When it expires, the agent stops
the command and its child processes (using a process group on Linux/macOS or
`taskkill /T /F` on Windows), then reports the job as `failed` with
`exit_code: -1`.

The end of the job's stderr contains a line like:

```text
relay: job timed out after 5s
```

Any stdout and stderr captured before the deadline are preserved. Once the
result is reported, the agent is available for the next job.

`relay status JOB_ID` shows `limit=5s` and, while the job is running, a
`left=` countdown. The job-details view in `relay dash` displays the same
limit and remaining time, refreshing every two seconds. After completion,
the limit remains visible but the countdown disappears.

## Manual verification on Windows

Before testing, have `relayd` running, log in to a pool using `relay login`,
and make sure at least one `relay-agent` is connected to that pool. Run the
following commands in PowerShell from an environment where `relay` is on
your PATH (or prefix them with `uv run` from the `cli` directory).

1. Submit a job that should run for about a minute but has a five-second limit:

   ```powershell
   relay submit --timeout 5s -- ping 127.0.0.1 -n 60
   ```

2. Copy the returned job ID. While it is running, check its status or open
   the dashboard and press Enter on that job:

   ```powershell
   relay status JOB_ID
   relay dash
   ```

   The limit should show `5s`, and the remaining time should decrease.

3. After the deadline, verify the job failed and its log ends with the
   timeout message:

   ```powershell
   relay status JOB_ID
   relay logs JOB_ID
   ```

4. Verify the agent can accept another job immediately after reporting the
   failure:

   ```powershell
   relay submit --wait -- cmd.exe /c echo next-job
   ```

   The second job should complete successfully on the same available pool.

The automated Go agent tests also verify that a child process does not
survive its parent's timeout.

## Manual verification on Linux or macOS

With a scheduler and agent running in your pool, use the equivalent commands:

```sh
relay submit --timeout 5s -- sleep 60
relay status JOB_ID
relay logs JOB_ID
relay submit --wait -- uname -n
```

The first job should be `failed`, with exit code `-1` and the timeout line in
stderr; the follow-up job should complete.

## Automated tests

From the repository root:

```sh
go test ./internal/store ./internal/api ./internal/agent -count=1
go test ./...
```

From the `cli` directory:

```sh
uv run pytest tests/test_timeout_display.py -v
uv run pytest -v
uv run ruff check .
```

The checks cover the API timeout boundaries and persistence, timeout parsing,
agent deadlines, process-tree cleanup, failure reporting, and the CLI and
dashboard display.
