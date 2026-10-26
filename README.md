
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

Relay has three central components: `relayd`, the scheduler, `relay-agent`, which is how a machine lends itself to a pool, and the `relay` CLI is for both admins and users to easily manage pools and jobs. 

Binaries for Linux, macOS, and Windows are on the [releases page](https://github.com/cachebag/relay/releases/latest). 

In order to use the CLI, you need [uv](https://docs.astral.sh/uv/getting-started/installation/).

**Install.** You can install all binaries via the CLI 

```sh
uv tool install "git+https://github.com/cachebag/relay#subdirectory=cli"
relay install
```

`relayd` and `relay-agent` are meant to run as background processes on your machine. You can pass `--agent-only` if you only plan on contributing to a pool. Running the scheduler is meant for those who plan to adminstrate / create their own pool and invite others.

`relay install` prints where the binaries went (`~/.config/relay/bin` on Linux, `~/Library/Application Support/relay/bin` on macOS, `%AppData%\relay\bin` on Windows). The examples below assume that folder is on your `PATH`; otherwise use the full path.

**Contribute a machine.** Run the agent with the address and pool token your admin gave you:

```sh
relay-agent --scheduler http://<scheduler address>:8080 --pool-token <pool token>
```

**Submit jobs.** Log in once, then submit:

```sh
relay login --scheduler http://<scheduler address>:8080 --pool-token <pool token> --name <your name>
relay submit --wait -- uname -n
```

**Run a pool.** Start the scheduler in the background and create a pool. The admin token relayd prints on first start is saved for you:

```sh
relay scheduler start      # --public for an address that works from anywhere, see docs/TAILSCALE.md
relay pool create my-pool
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
