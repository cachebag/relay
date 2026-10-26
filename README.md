
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://github.com/user-attachments/assets/c4043fe3-7570-4408-a93e-7644c74e6a86">
    <img alt="relay" src="https://github.com/user-attachments/assets/91f7d398-c90b-474b-8c20-1a2d643ebb73" width="360">
  </picture>
</p>

Turn idle laptops into a shared compute pool. Submit jobs from
anywhere, run them macOS or Linux.

The idea of `relay` is that you can volunteer a machine you aren't
using and it becomes a node. Anyone in the pool can submit jobs to it.

Contributing also earns credits; submitting spends them, and the more
points someone has, the higher priority their jobs get.


# Quickstart

Relay has three parts: `relayd` (the scheduler), `relay-agent` (runs on contributed machines), and the `relay` CLI (for admins and submitters). Binaries for Linux, macOS, and Windows are on the [releases page](https://github.com/cachebag/relay/releases/latest). The CLI needs [uv](https://docs.astral.sh/uv/getting-started/installation/).

**Contribute a machine.** Get the agent (`relay install --agent-only` with the CLI, or from the releases page) and run it with the address and pool token your admin gave you:

```sh
./relay-agent --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

**Submit jobs.** Install the CLI, log in once, then submit:

```sh
uv tool install "git+https://github.com/cachebag/relay#subdirectory=cli"
relay login --scheduler http://<scheduler address>:8080 --pool-token <pool token> --name <your name>
relay submit --wait -- uname -n
```

**Run a pool.** Start the scheduler and create a pool:

```sh
./relayd
relay pool create my-pool --admin-token <server admin token>
```

## Guides

| I want to... | Read |
|---|---|
| Lend my machine to a pool, or leave one | [docs/CONTRIBUTE.md](docs/CONTRIBUTE.md) |
| Install the CLI and run jobs | [docs/SUBMIT.md](docs/SUBMIT.md) |
| Run a scheduler, create pools, rotate tokens | [docs/ADMIN.md](docs/ADMIN.md) |
| Put my scheduler on the internet | [docs/TAILSCALE.md](docs/TAILSCALE.md) |
| Use the HTTP API directly | [docs/api.md](docs/api.md) |

# For developers

Please read the [setup guide](docs/SETUP.md) to get your development environment setup.

## Stack

* Go (scheduler and node agent)
* Python (CLI)
* Tailscale (networking)
* SQLite (job and node state)
