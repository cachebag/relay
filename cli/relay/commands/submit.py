import httpx


def register(sub) -> None:
    p = sub.add_parser("submit", help="submit a job to the pool")
    p.add_argument("command", nargs="+", help="command to run, after --")
    p.set_defaults(func=run)


def submit(
    scheduler: str, command: list[str], client: httpx.Client | None = None
) -> dict:
    client = client or httpx.Client(timeout=5)
    r = client.post(f"{scheduler}/jobs", json={"command": command})
    r.raise_for_status()
    return r.json()


def run(args) -> None:
    job = submit(args.scheduler, args.command)
    print(f"{job['id']}  {job['status']}")
