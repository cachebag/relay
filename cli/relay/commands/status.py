from datetime import UTC, datetime
from math import ceil

import httpx

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("status", help="show a job's status")
    p.add_argument("job_id")
    p.set_defaults(func=run)


def requires(job: dict) -> str:
    """The required OS for the job, or an empty string if it can run on any OS."""
    return (job.get("requires") or {}).get("os", "")


def fetch(
    scheduler: str, job_id: str, token: str, client: httpx.Client | None = None
) -> dict:
    """Get one job. The scheduler only returns jobs in the token's pool."""
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/jobs/{job_id}", headers=profile.bearer(token))
    r.raise_for_status()
    return r.json()


def duration(seconds: float) -> str:
    """Format a run time the way people say it: 45s, 3m12s, 1h2m."""
    s = int(seconds)
    if s < 60:
        return f"{s}s"
    if s < 3600:
        return f"{s // 60}m{s % 60:02d}s"
    return f"{s // 3600}h{(s % 3600) // 60}m"


def runtime(job: dict) -> str | None:
    """How long the job ran, or has been running. None if it never started."""
    started = job.get("started_at")
    if not started:
        return None
    start = datetime.fromisoformat(started)
    finished = job.get("finished_at")
    end = datetime.fromisoformat(finished) if finished else datetime.now(UTC)
    return duration((end - start).total_seconds())


def timeout_seconds(job: dict) -> int:
    """Return the job's per-attempt timeout in seconds."""
    return job["timeout_seconds"]


def format_limit(seconds: int) -> str:
    """Format a timeout compactly, for example 1h, 10m or 1m30s."""
    hours, remainder = divmod(seconds, 3600)
    minutes, seconds = divmod(remainder, 60)

    # Leave out units equal to zero, except when the entire value is 0s.
    return (
        (f"{hours}h" if hours else "")
        + (f"{minutes}m" if minutes else "")
        + (f"{seconds}s" if seconds or not (hours or minutes) else "")
    )


def time_limit(job: dict) -> str:
    """Return the configured time limit for display in the CLI and dashboard."""
    return format_limit(timeout_seconds(job))


def time_left(job: dict, now: datetime | None = None) -> str | None:
    """Return remaining time for a running job, or None otherwise."""

    # Queued and finished jobs do not have an active execution deadline.
    # A running job also needs started_at to calculate its elapsed time.
    if job.get("status") != "running" or not job.get("started_at"):
        return None

    start = datetime.fromisoformat(job["started_at"])
    now = now if now is not None else datetime.now(UTC)

    # Protect against a start time slightly ahead of the local clock.
    elapsed = max(0.0, (now - start).total_seconds())

    # Round up partial seconds so the display does not show 0s before the
    # deadline. Once the limit expires, stop at 0s instead of going negative.
    seconds_left = max(0, ceil(timeout_seconds(job) - elapsed))

    return format_limit(seconds_left)


def run(args) -> None:
    prof = profile.require_login()
    j = fetch(args.scheduler, args.job_id, prof["pool_token"])

    node = j["node_id"] or "-"
    attempts = j.get("attempts", 0)
    ran = runtime(j)
    remaining = time_left(j)

    if ran is None:
        timing = ""
    elif j["status"] == "running":
        timing = f"running for {ran}  "
    else:
        timing = f"ran {ran}  "

    state = j["status"]
    if requires(j):
        state += f" (requires {requires(j)})"

    # Every job shows its limit; only running jobs show a countdown.
    print(
        f"{j['id']}  {state:<9} node={node}  attempts={attempts}  "
        f"limit={time_limit(j)}  "
        f"{'left=' + (remaining or '-') + '  ' if j['status'] == 'running' else ''}"
        f"{timing}{' '.join(j['command'])}"
    )
