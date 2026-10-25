import sys

import httpx

from relay import profile


def register(sub) -> None:
    p = sub.add_parser("logs", help="show a job's output")
    p.add_argument("id", help="job id")
    p.set_defaults(func=run)


def fetch_output(
    scheduler: str,
    job_id: str,
    token: str,
    client: httpx.Client | None = None,
) -> tuple[int, dict | None]:
    """Get a job's output. Returns (status code, body): 404 is no such job
    and 409 is not finished yet, both with no body. Other errors raise."""
    client = client or httpx.Client(timeout=5)
    r = client.get(
        f"{scheduler.rstrip('/')}/jobs/{job_id}/output",
        headers=profile.bearer(token),
    )
    if r.status_code in (404, 409):
        return r.status_code, None
    r.raise_for_status()
    return r.status_code, r.json()


def print_job_output(
    scheduler: str,
    job_id: str,
    token: str,
    client: httpx.Client | None = None,
) -> int:
    """Fetch and print a finished job's output; return its exit code."""
    code, result = fetch_output(scheduler, job_id, token, client)

    if code == 404:
        print("no such job", file=sys.stderr)
        sys.exit(1)

    if code == 409:
        print(f"job {job_id} has not finished", file=sys.stderr)
        sys.exit(1)
    assert result is not None

    stdout = result.get("stdout", "")
    stderr = result.get("stderr", "")
    exit_code = result.get("exit_code", 0)

    if stdout:
        print(stdout, end="")

    if stderr:
        print(stderr, end="", file=sys.stderr)

    return exit_code


def run(args, client: httpx.Client | None = None) -> None:
    """Show a job's output and exit with the remote job's exit code."""
    prof = profile.require_login()
    sys.exit(
        print_job_output(args.scheduler, args.id, prof["pool_token"], client=client)
    )
