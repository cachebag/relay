# relay

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

## Stack

* Go (scheduler and node agent)
* Python (CLI) 
* Tailscale (networking)
* SQLite (job and node state)
