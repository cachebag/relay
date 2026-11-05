import argparse
import re
import sys
import time
from decimal import ROUND_CEILING, Decimal

import httpx

from relay import profile
from relay.commands import logs, status

_DURATION_PART = re.compile(r"(\d+(?:\.\d*)?|\.\d+)(s|m|h)")

_SECONDS_PER_UNIT = {
    "s": Decimal(1),
    "m": Decimal(60),
    "h": Decimal(3600),
}

_MAX_JOB_TIMEOUT_SECONDS = 24 * 60 * 60


def parse_job_timeout(value: str) -> int:
    """Convert durations using s, m, h or plain seconds to whole seconds.

    Fractional durations round up because the API stores integer seconds.
    """
    if value.isascii() and value.isdecimal():
        seconds = int(value)
    else:
        total = Decimal(0)
        offset = 0

        while offset < len(value):
            part = _DURATION_PART.match(value, offset)

            if part is None:
                raise argparse.ArgumentTypeError(
                    "--timeout must be seconds or a duration such as 5s, 10m, or 1h30m"
                )

            total += Decimal(part.group(1)) * _SECONDS_PER_UNIT[part.group(2)]
            offset = part.end()

        seconds = int(total.to_integral_value(rounding=ROUND_CEILING))

    if not 1 <= seconds <= _MAX_JOB_TIMEOUT_SECONDS:
        raise argparse.ArgumentTypeError(
            "--timeout must be between 1 second and 24 hours"
        )

    return seconds


def register(sub) -> None:
    p = sub.add_parser("submit", help="submit a job to the pool")

    # Allows users to wait for a submitted job to finish.
    p.add_argument(
        "--wait",
        action="store_true",
        help="wait for the job to finish and print its output",
    )

    # Set the time limit enforced by the agent for each job attempt.
    p.add_argument(
        "--timeout",
        type=parse_job_timeout,
        metavar="DURATION",
        help="job time limit (default: 1h; e.g. 5s, 10m, 1h30m, or plain seconds)",
    )

    # Local waiting has a separate timeout; it never cancels the remote job.
    p.add_argument(
        "--wait-timeout",
        type=float,
        metavar="SECONDS",
        help="stop waiting after this many seconds without cancelling the job (requires --wait)",
    )

    # Restrict the job to run only on nodes with the specified
    # operating system.
    p.add_argument(
        "--os",
        choices=["linux", "darwin", "windows"],
        help="required operating system for the job",
    )

    p.add_argument("command", nargs="+", help="command to run, after --")
    p.set_defaults(func=run)


def submit(
    scheduler: str,
    command: list[str],
    token: str,
    submitter: str,
    client: httpx.Client | None = None,
    os: str | None = None,
    timeout_seconds: int | None = None,
) -> dict:
    """Submit a job using the logged-in pool token and user name."""
    client = client or httpx.Client(timeout=5)

    body = {
        "command": command,
        "submitter": submitter,
    }

    if os is not None:
        body["requires"] = {"os": os}

    if timeout_seconds is not None:
        body["timeout_seconds"] = timeout_seconds

    r = client.post(
        f"{scheduler.rstrip('/')}/jobs",
        headers={"Authorization": f"Bearer {token}"},
        json=body,
    )

    r.raise_for_status()
    return r.json()


def _job_state(job: dict) -> tuple:
    """Track changes in status, assigned node, or attempt count."""
    return job["status"], job.get("node_id") or "", job.get("attempts", 0)


def _format_state(job: dict) -> str:
    """Format a short progress line, including retries and assigned nodes."""
    state, node, attempts = _job_state(job)

    # Show when a job has been requeued after a previous attempt.
    if state == "queued" and attempts > 0:
        return f"requeued, attempt {attempts + 1}"

    # Include the assigned node and attempt count when available.
    line = state
    if state in ("assigned", "running") and node:
        line += f" on {node}"
    if attempts > 1:
        line += f", attempt {attempts}"
    return line


def _print_wait_hint(job_id: str) -> None:
    """Explain how to check a job after waiting stops."""
    print(
        f"still running, check later with relay status {job_id}",
        file=sys.stderr,
    )


def wait_for_job(
    scheduler: str,
    job_id: str,
    token: str,
    client: httpx.Client | None = None,
    poll_interval: float = 1.0,
    initial_job: dict | None = None,
    timeout: float | None = None,
) -> int:
    """Wait for completion without cancelling a job on timeout or Ctrl+C."""

    # Remember the previous state to avoid printing duplicate updates.
    last_state = _job_state(initial_job) if initial_job is not None else None

    # Use a monotonic clock so system clock changes cannot affect the timeout.
    deadline = time.monotonic() + timeout if timeout is not None else None

    while True:
        try:
            # Stop polling once the timeout expires, leaving the job running.
            if deadline is not None and time.monotonic() >= deadline:
                _print_wait_hint(job_id)
                return 124

            # Request the latest job information from the scheduler.
            job = status.fetch(scheduler, job_id, token, client=client)
            current_state = _job_state(job)

            # Only print an update when the job's state has changed.
            if current_state != last_state:
                print(_format_state(job), flush=True)
                last_state = current_state

            # Stop waiting once the job finishes.
            if job["status"] in ("done", "failed"):
                break

            # Sleep no longer than the remaining timeout before polling again.
            if deadline is not None:
                remaining = deadline - time.monotonic()

                if remaining <= 0:
                    _print_wait_hint(job_id)
                    return 124

                time.sleep(min(poll_interval, remaining))
            else:
                time.sleep(poll_interval)

        # Ctrl+C stops local polling without cancelling the remote job.
        except KeyboardInterrupt:
            _print_wait_hint(job_id)
            return 130

    # Print the output and return the job's exit code, just like relay logs.
    return logs.print_job_output(scheduler, job_id, token, client=client)


def run(args) -> None:
    """Submit a job using the saved login profile."""

    # Argparse already validated and converted the job limit to seconds.
    timeout_seconds = getattr(args, "timeout", None)

    wait_timeout = getattr(args, "wait_timeout", None)

    if wait_timeout is not None:
        if not getattr(args, "wait", False):
            print("relay: --wait-timeout requires --wait", file=sys.stderr)
            sys.exit(2)

        if not 0 < wait_timeout < float("inf"):
            print(
                "relay: --wait-timeout must be a positive number of seconds",
                file=sys.stderr,
            )
            sys.exit(2)

    prof = profile.load()

    token = prof.get("pool_token")
    name = prof.get("name")

    # A saved pool token and user name are required.
    if (
        not isinstance(token, str)
        or not token.strip()
        or not isinstance(name, str)
        or not name.strip()
    ):
        print("relay: not logged in. Run 'relay login' first.", file=sys.stderr)
        sys.exit(1)

    try:
        job = submit(
            args.scheduler,
            args.command,
            token,
            name,
            os=getattr(args, "os", None),
            timeout_seconds=timeout_seconds,
        )
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 403:
            print(
                "relay: invalid pool token. Run 'relay login' again.",
                file=sys.stderr,
            )
        else:
            print(f"relay: job submission failed: {exc}", file=sys.stderr)
        sys.exit(1)

    print(f"{job['id']}  {job['status']}", flush=True)

    # Wait for completion if requested, using only the local polling timeout.
    # Otherwise, return immediately after submitting the job.
    if getattr(args, "wait", False):
        sys.exit(
            wait_for_job(
                args.scheduler,
                job["id"],
                token,
                initial_job=job,
                timeout=wait_timeout,
            )
        )
