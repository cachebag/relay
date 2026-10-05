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


# For developers

Please read the [setup guide](docs/SETUP.md) to get your development environment setup.


# Contribute a machine

First download the binary for your OS from the latest release.

The scheduler address can be set with the `RELAY_SCHEDULER` environment variable. If it is not set, Relay defaults to `http://localhost:8080`. The `--scheduler` command-line flag can still be used and takes precedence over the environment variable.

### Linux and macOS

Set the scheduler address:

```bash
export RELAY_SCHEDULER="http://<scheduler>:8080"
```

Then run:

```bash
./relay-agent --pool-token <token>
```

> macOS will refuse to run an unsigned download until you right-click → Open once.

### Windows

Set the scheduler address from PowerShell:

```powershell
$env:RELAY_SCHEDULER="http://<scheduler>:8080"
```

Then run:

```powershell
.\relay-agent-windows-amd64.exe --pool-token <token>
```

You can also provide the scheduler directly with `--scheduler`:

```bash
./relay-agent --scheduler http://<scheduler>:8080 --pool-token <token>
```


## Stack

* Go (scheduler and node agent)
* Python (CLI)
* Tailscale (networking)
* SQLite (job and node state)