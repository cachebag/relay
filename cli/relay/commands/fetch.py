import io
import sys
import tarfile
from pathlib import Path

import httpx

from relay import profile
from relay.commands import status as status_cmd

DEFAULT_ROOT = "relay-out"
FINISHED = ("done", "failed")


class FetchFailed(Exception):
    pass


class FilesExist(FetchFailed):
    pass


def register(sub) -> None:
    p = sub.add_parser("fetch", help="download a job's output files")
    p.add_argument("id", help="job id")
    p.add_argument(
        "--to",
        default=None,
        help=f"folder to unpack into (default: ./{DEFAULT_ROOT}/<job id>)",
    )
    p.add_argument(
        "--force",
        action="store_true",
        help="overwrite files that are already there",
    )
    p.set_defaults(func=run)


def default_folder(job_id: str) -> Path:
    return Path(DEFAULT_ROOT) / job_id


def download(
    scheduler: str,
    job_id: str,
    token: str,
    client: httpx.Client | None = None,
) -> bytes | None:
    client = client or httpx.Client(timeout=300)
    r = client.get(
        f"{scheduler.rstrip('/')}/jobs/{job_id}/output/files",
        headers=profile.bearer(token),
    )
    if r.status_code == 404:
        return None
    r.raise_for_status()
    return r.content


def unpack(bundle: bytes, dest: Path, force: bool = False) -> list[Path]:
    try:
        with tarfile.open(fileobj=io.BytesIO(bundle), mode="r:gz") as tar:
            return _unpack(tar, dest, force)
    except (tarfile.ReadError, EOFError) as exc:
        raise FetchFailed(f"the output bundle is not a valid tar: {exc}") from exc


def _unpack(tar: tarfile.TarFile, dest: Path, force: bool) -> list[Path]:
    files = [m for m in tar.getmembers() if m.isfile()]
    targets = []
    for m in files:
        try:
            safe = tarfile.data_filter(m, str(dest))
        except tarfile.FilterError as exc:
            raise FetchFailed(f"unsafe path in output bundle: {m.name}") from exc
        # The filter strips a leading slash, so this is where extract writes.
        targets.append(dest / safe.name)

    if not force:
        existing = [t for t in targets if t.exists()]
        if existing:
            more = f" (and {len(existing) - 1} more)" if len(existing) > 1 else ""
            raise FilesExist(f"{existing[0]} already exists{more}")

    dest.mkdir(parents=True, exist_ok=True)
    for m in files:
        tar.extract(m, dest, filter="data")
    return targets


def fetch_files(
    scheduler: str,
    job_id: str,
    token: str,
    dest: Path | None = None,
    force: bool = False,
    client: httpx.Client | None = None,
    job: dict | None = None,
) -> tuple[Path, list[Path]]:
    dest = dest or default_folder(job_id)
    if job is None:
        try:
            job = status_cmd.fetch(scheduler, job_id, token, client=client)
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code == 404:
                raise FetchFailed("no such job") from exc
            raise
    if job.get("status") not in FINISHED:
        raise FetchFailed(f"job {job_id} has not finished")
    if not job.get("output_files"):
        return dest, []

    bundle = download(scheduler, job_id, token, client=client)
    if bundle is None:
        return dest, []
    return dest, unpack(bundle, dest, force=force)


def run(args, client: httpx.Client | None = None) -> None:
    prof = profile.require_login()
    dest = Path(args.to) if args.to else None
    try:
        folder, files = fetch_files(
            args.scheduler,
            args.id,
            prof["pool_token"],
            dest=dest,
            force=args.force,
            client=client,
        )
    except FilesExist as exc:
        print(f"relay: {exc}, use --force to overwrite", file=sys.stderr)
        sys.exit(1)
    except FetchFailed as exc:
        print(f"relay: {exc}", file=sys.stderr)
        sys.exit(1)

    if not files:
        print(f"job {args.id} has no output files")
        return
    for f in files:
        print(f)
    print(f"{len(files)} file{'s' if len(files) != 1 else ''} in {folder}")
