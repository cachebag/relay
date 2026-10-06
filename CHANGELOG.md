# Changelog

All notable changes to relay are recorded here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each entry links the pull request that made the change. Changes that landed
without a pull request link their commit instead.

## [Unreleased]

### Added

- **Agent: run assigned jobs.** After each heartbeat, `relay-agent` asks the
  scheduler for its next job, runs the command directly on the machine and
  reports the exit code, stdout and stderr. The job runs in the background so
  heartbeats keep going while it runs, and the agent won't take another job
  until the current one is reported. Each output stream is capped at 1 MiB. A
  command that can't be started (or is empty) reports exit code `-1`. Ctrl-C
  or `SIGTERM` stops the running job and leaves it unreported, so the
  scheduler hands it out again. ([#58](https://github.com/cachebag/relay/pull/58))
- **Agent: unreported results survive outages and restarts.** A finished
  result is saved to `<user cache dir>/relay/pending.json` and retried every
  interval until the scheduler accepts it. While a result is waiting, the agent
  doesn't fetch new work. On startup, a saved result is sent before anything
  else. A `409` (the job was reassigned), or any other `4xx`, discards the
  result. A network error or `5xx` keeps it.
  ([#68](https://github.com/cachebag/relay/pull/68))
- **API: hand nodes their work and serve job output.**
  - `GET /nodes/{id}/jobs/next` returns the oldest job assigned to that node
    and moves it from `assigned` to `running`, or `204` if there is none.
  - `GET /jobs/{id}/output` returns a finished job's exit code, stdout and
    stderr, or `409` if the job hasn't finished.
  - `POST /jobs/{id}/result` now requires `exit_code`, stores the output, and
    marks the job `done` (exit code 0) or `failed`. Bodies over the size limit
    get `413`.
  - Job listings no longer include output.

  ([#64](https://github.com/cachebag/relay/pull/64))
- **API: stale results and node status.** `POST /jobs/{id}/result` accepts a
  node's result for its job, and rejects it with `409` if the job is no longer
  active on that node for that attempt. `GET /nodes` now includes a
  server-computed `status` of `online` or `offline`.
  ([#52](https://github.com/cachebag/relay/pull/52))
- **CLI: job visibility.**
  - `relay jobs [--pool <id>]` lists jobs.
  - `relay logs <id>` prints a job's output and exits with the job's exit
    code. It reports an error if the job hasn't finished or doesn't exist.
  - `relay dash` shows a jobs table alongside the nodes.

  ([#50](https://github.com/cachebag/relay/pull/50))
- **`RELAY_SCHEDULER` environment variable.** `relay` and `relay-agent` take
  the scheduler address from `--scheduler`, then `RELAY_SCHEDULER`, then
  `http://localhost:8080`. When the scheduler can't be reached, the CLI prints
  `relay: can't reach scheduler at <url>` instead of a Python traceback.
  ([#65](https://github.com/cachebag/relay/pull/65))
- **The CLI ships with each release.** Tagging a release builds the `relay` CLI
  as a Python wheel, stamped with the tag's version, and attaches it to the
  GitHub release. Release builds of `relayd` and `relay-agent` now report their
  real version too, and `relay-agent` gains `--version`. CI installs the built
  wheel and smoke-tests it on Ubuntu and Windows.
  ([#66](https://github.com/cachebag/relay/pull/66))
- **`tools/resetdb`, a developer-only tool** that deletes the local `relay.db`
  and its WAL/SHM files. It isn't part of any release binary or the CLI.
  ([#67](https://github.com/cachebag/relay/pull/67))

### Changed

- **Pools and jobs persist in SQLite.** Nodes, pools and jobs now share one
  SQLite database, so `relayd` keeps all of its state across a restart.
  Creating a pool whose ID or token already exists returns an error.
  ([#54](https://github.com/cachebag/relay/pull/54))
- **README quickstart for contributors, admins and submitters.** Added the
  first role-by-role quickstart ([#66](https://github.com/cachebag/relay/pull/66)),
  then switched downloads to `curl` and installing the CLI to
  `uv tool install "git+https://github.com/cachebag/relay#subdirectory=cli"`
  ([#73](https://github.com/cachebag/relay/pull/73)).

### Fixed

- **`database is locked` (`SQLITE_BUSY`) under concurrent writes.** Heartbeats
  and scheduler updates could fail when they hit the database at the same
  time. SQLite now runs in WAL mode with a 5 s busy timeout
  ([986b097](https://github.com/cachebag/relay/commit/986b097), in
  [#54](https://github.com/cachebag/relay/pull/54)), and uses a single
  connection, so writes queue in `relayd` instead of colliding in SQLite
  ([#69](https://github.com/cachebag/relay/pull/69)).

## [0.1.1] - 2026-09-28

### Added

- **Jobs.** A `Job` type, `JobStore` interface and in-memory store. Jobs move
  through `queued`, `assigned`, `running`, `done` and `failed`. The store
  copies `Command` so callers can't change a stored job, lists jobs in a stable
  order, and rejects duplicate creates and updates to missing jobs.
  ([#31](https://github.com/cachebag/relay/pull/31))
- **Job endpoints.** `POST /jobs` queues a command and returns its 12-character
  ID. `GET /jobs/{id}` and `GET /jobs` read jobs back.
  ([#38](https://github.com/cachebag/relay/pull/38))
- **Scheduler loop.** Every 2 seconds, `relayd` assigns queued jobs to online
  nodes, oldest job first, spread round-robin across nodes. Offline nodes get
  nothing. ([#39](https://github.com/cachebag/relay/pull/39))
- **Requeue from offline nodes.** A job on a node that stops heartbeating goes
  back in the queue and can be placed on another node in the same tick. After
  3 placements it is marked `failed` instead. A job on a node that was never
  registered counts as lost. `Job.Attempts` counts placements, and
  `relay status` shows `attempts=N`. "Online" now has one definition, a
  heartbeat within the last 30 s (`store.NodeTimeout`).
  ([#48](https://github.com/cachebag/relay/pull/48))
- **CLI: `relay submit -- <command>` and `relay status <id>`.**
  ([#47](https://github.com/cachebag/relay/pull/47))
- **CLI: pools.** `relay pool create <name>` prints a join token,
  `relay nodes --pool <id>` filters by pool, and `relay nodes` shows an
  online/offline column. ([#36](https://github.com/cachebag/relay/pull/36))
- **CLI: `relay dash`,** a live terminal dashboard of nodes, built with
  Textual. ([#37](https://github.com/cachebag/relay/pull/37))
- **`relayd --port`.** The listen port was fixed at 8080. It can now be set,
  values outside 0–65535 are rejected, and `0` lets the OS pick a free port.
  ([#46](https://github.com/cachebag/relay/pull/46))

### Changed

- **README: "Contribute a machine" section** with run instructions for the
  agent binaries. ([#35](https://github.com/cachebag/relay/pull/35); the rest
  of that PR shipped in 0.1.0.)

## [0.1.0] - 2026-09-19

First release, published as the `v0.1.0-beta` pre-release. `v0.1.0` and
`v0.1.0-beta` point at the same commit.

### Added

- **Project scaffold.** Go module with `cmd/relayd`, `cmd/relay-agent` and
  `internal/*` packages, a Python CLI managed with `uv`, a setup guide, and a
  task issue template. ([#1](https://github.com/cachebag/relay/pull/1))
- **README** with the project description and banner.
  ([7d26e84](https://github.com/cachebag/relay/commit/7d26e84),
  [a5a9aad](https://github.com/cachebag/relay/commit/a5a9aad))
- **CI.** GitHub Actions on every pull request and push to `main`, on Ubuntu
  and Windows. Go: `build`, `vet`, `test`. CLI: `uv sync`, `ruff`, `pytest`.
  ([#13](https://github.com/cachebag/relay/pull/13),
  renamed in [#26](https://github.com/cachebag/relay/pull/26))
- **`GET /healthz`** on `relayd`. ([#14](https://github.com/cachebag/relay/pull/14))
- **`--version`** for the CLI ([#15](https://github.com/cachebag/relay/pull/15))
  and `relayd` ([#17](https://github.com/cachebag/relay/pull/17)).
- **Node registry.** A `Node` type, a `Registry` interface and an in-memory
  store ([#16](https://github.com/cachebag/relay/pull/16)), then a SQLite store
  in `relay.db` so registered nodes survive a restart
  ([#18](https://github.com/cachebag/relay/pull/18)).
- **Heartbeat and node list endpoints.** `POST /nodes/heartbeat` registers or
  updates a node, and `GET /nodes` lists them. Invalid JSON or a missing `id`
  gets `400`. ([#19](https://github.com/cachebag/relay/pull/19))
- **CLI: `relay nodes`** lists the nodes the scheduler knows about.
  ([#20](https://github.com/cachebag/relay/pull/20))
- **Pools.** A `Pool` type, `PoolStore` interface and in-memory store, with
  generated IDs and join tokens. Nodes belong to a pool.
  ([#32](https://github.com/cachebag/relay/pull/32))
- **Pool endpoints.** `POST /pools` creates a pool and returns its join token.
  `GET /pools` lists pools without their tokens. A heartbeat must carry a valid
  `pool_token`, or it gets `403`, and the server assigns the node's pool from
  the token. `GET /nodes?pool=<id>` filters by pool. API reference added in
  `docs/api.md`. ([#34](https://github.com/cachebag/relay/pull/34))
- **Agent heartbeats.** `relay-agent` collects the machine's hostname, OS,
  architecture, CPU count and memory and sends a heartbeat every 10 seconds.
  ([#33](https://github.com/cachebag/relay/pull/33))
- **Agent pool tokens and release binaries.** `relay-agent` takes
  `--scheduler` and a required `--pool-token`. Pushing a `v*` tag publishes
  `relay-agent` and `relayd` for `linux-amd64`, `darwin-arm64`,
  `darwin-amd64` and `windows-amd64`.
  ([#35](https://github.com/cachebag/relay/pull/35); this release was tagged
  from that PR's branch before it merged.)

[Unreleased]: https://github.com/cachebag/relay/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/cachebag/relay/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/cachebag/relay/commits/v0.1.0
