import httpx


def register(sub) -> None:
    p = sub.add_parser("pool", help="manage pools")
    ps = p.add_subparsers(dest="pool_command", required=True)
    c = ps.add_parser("create", help="create a pool and print its join token")
    c.add_argument("name")
    c.set_defaults(func=run_create)


def create(scheduler: str, name: str, client: httpx.Client | None = None) -> dict:
    client = client or httpx.Client(timeout=5)
    r = client.post(f"{scheduler}/pools", json={"name": name})
    r.raise_for_status()
    return r.json()


def run_create(args) -> None:
    p = create(args.scheduler, args.name)
    print(f"pool id:    {p['id']}")
    print(f"join token: {p['token']}")
    print()
    print("share the token with contributors; they run:")
    print("relay-agent --scheduler {args.scheduler} --pool-token {p['token']}")
