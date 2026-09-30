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


def run(args) -> None:
    j = fetch(args.scheduler, args.job_id)
    node = j["node_id"] or "-"
    attempts = j.get("attempts", 0)
    print(
        f"{j['id']}  {j['status']:<9} node={node}  attempts={attempts}  "
        f"{' '.join(j['command'])}"
    )

