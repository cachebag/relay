from datetime import UTC, datetime

import httpx


def register(sub) -> None:
    p = sub.add_parser("status", help="show a job's status")
    p.add_argument("job_id")
    p.set_defaults(func=run)


def fetch(scheduler: str, job_id: str, client: httpx.Client | None = None) -> dict:
    client = client or httpx.Client(timeout=5)
    r = client.get(f"{scheduler}/jobs/{job_id}")
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


def run(args) -> None:
    j = fetch(args.scheduler, args.job_id)
    node = j["node_id"] or "-"
    attempts = j.get("attempts", 0)
    ran = runtime(j)
    if ran is None:
        timing = ""
    elif j["status"] == "running":
        timing = f"running for {ran}  "
    else:
        timing = f"ran {ran}  "
    print(
        f"{j['id']}  {j['status']:<9} node={node}  attempts={attempts}  "
        f"{timing}{' '.join(j['command'])}"
    )
