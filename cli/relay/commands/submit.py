
import sys
import time

import httpx

from relay import profile
from relay.commands import logs, status


def register(sub) -> None:
    p = sub.add_parser("submit", help="submit a job to the pool")

    # Allows users to wait for a submitted job to finish.
    p.add_argument(
        "--wait",
        action="store_true",
        help="wait for the job to finish and print its output",
    )

    p.add_argument("command", nargs="+", help="command to run, after --")
    p.set_defaults(func=run)


def submit(
    scheduler: str,
    command: list[str],
    token: str,
    submitter: str,
    client: httpx.Client | None = None,
) -> dict:
    """Submit a job using the logged-in pool token and user name."""
    client = client or httpx.Client(timeout=5)

    r = client.post(
        f"{scheduler.rstrip('/')}/jobs",
        headers={"Authorization": f"Bearer {token}"},
        json={
            "command": command,
            "submitter": submitter,
        },
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


def wait_for_job(
    scheduler: str,
    job_id: str,
    client: httpx.Client | None = None,
    poll_interval: float = 1.0,
    initial_job: dict | None = None,
) -> int:
    """Poll until completion, print new states, then display the job output."""

    # Remember the previous state to avoid printing duplicate updates.
    last_state = _job_state(initial_job) if initial_job is not None else None

    while True:
        # Request the latest job information from the scheduler.
        job = status.fetch(scheduler, job_id, client=client)
        current_state = _job_state(job)

        # Only print an update when the job's state has changed.
        if current_state != last_state:
            print(_format_state(job), flush=True)
            last_state = current_state

        # Once finished, print the output and return the job's exit code.
        if job["status"] in ("done", "failed"):
            return logs.print_job_output(scheduler, job_id, client=client)

        # Wait one second before checking the job again.
        time.sleep(poll_interval)


def run(args) -> None:
    """Submit a job using the saved login profile."""
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

    # If --wait is enabled, monitor the job and exit with its result.
    # Otherwise, return immediately after submitting the job.
    if getattr(args, "wait", False):
        sys.exit(wait_for_job(args.scheduler, job["id"], initial_job=job))
