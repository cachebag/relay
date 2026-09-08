# relay

Turn idle laptops into a shared compute pool. Submit jobs from
anywhere, run them macOS or Linux.

The idea of `relay` is that you can volunteer a machine you aren't
using and it becomes a node. Anyone in the pool can submit jobs to it.

Contributing also earns credits; submitting spends them, and the more
points someone has, the higher priority their jobs get.

# For developers

Please read the [setup guide](docs/SETUP.md) to get your development environment setup.

## Stack

* Go (scheduler and node agent)
* Python (CLI) 
* Tailscale (networking)
* SQLite (job and node state)
