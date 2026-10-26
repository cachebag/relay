# Run a scheduler and manage a pool

The admin runs `relayd` somewhere pool members can reach, creates the pool, and hands out its join token. You need the `relay` CLI, which needs [uv](https://docs.astral.sh/uv/getting-started/installation/). See [SUBMIT.md](SUBMIT.md) for installing it.

Release assets are named `relayd-<os>-<arch>` (`.exe` on Windows), where `<os>-<arch>` is one of `linux-amd64`, `darwin-arm64`, `darwin-amd64`, `windows-amd64`. The examples below use `linux-amd64`; swap in yours.

## Run the scheduler

With the CLI installed, `relay install` downloads `relayd` and `relay-agent` for this machine into `relay/bin` under your config folder and prints where. Or fetch `relayd` by hand:

```sh
curl -LO https://github.com/cachebag/relay/releases/latest/download/relayd-linux-amd64
chmod +x relayd-linux-amd64
./relayd-linux-amd64                  # listens on :8080; leave it running
```

The first time it starts, `relayd` prints a server admin token. Save it. It is the only thing that can create pools on this scheduler, so strangers can't make pools on your server once it is reachable from the internet. If you lose it, `./relayd-linux-amd64 --new-admin-token` prints a new one and exits.

## Create a pool

```sh
# in another terminal, with the CLI installed
relay pool create my-pool --admin-token <server admin token>
```

This prints the pool's join token, which you send to contributors and submitters, and the pool's admin token, which only you keep. Instead of `--admin-token` you can set `RELAY_ADMIN_TOKEN`.

If you create a pool using a scheduler on your own machine (for example,
`http://localhost:8080`), Relay prints an invitation like this:

```text
relay-agent --scheduler http://<your address>:8080 --pool-token <join token>
replace <your address> with an address contributors can reach, for example your Tailscale name.
```

Before sharing the command, replace `<your address>` with the scheduler's reachable
LAN IP address or Tailscale hostname. The placeholder is not a usable address,
and `localhost` on a contributor's computer refers to their own computer, not
the admin's scheduler. Relay keeps the original port in the invitation. If you
already connect using a non-local scheduler address, the command displays that
address unchanged without the replacement hint.

Every other command works on the pool you are logged in to (`relay login`), and sends its join token with each request. Nobody without a pool's join token can see its machines, jobs, or output.

## Rotate the join token

`relay pool rotate-token` prints the same contributor command and address hint
when applicable. After rotating, distribute the new join token and restart
contributor agents, since the old join token no longer works. Never share the
server or pool admin token with contributors.

## Run relayd on the internet

Contributors on other networks can't reach a scheduler on your laptop. `relayd --public` gets it a public `https://` address through [Tailscale Funnel](https://tailscale.com/kb/1223/funnel). No router changes, no domain, no certificate, and nothing to install on contributors' machines. Only you need a Tailscale account, and the free plan is enough.

One-time setup, in the [Tailscale admin console](https://login.tailscale.com/admin). There is a [step-by-step guide with screenshots](TAILSCALE.md).

1. **DNS**: make sure MagicDNS and HTTPS Certificates are enabled.
2. **Settings, Keys**: generate an auth key. Tick **Reusable** so a restart keeps the same address.

Then run `relayd` with the key:

```sh
export TS_AUTHKEY=tskey-auth-...
./relayd-linux-amd64 --public          # add --name <hostname> to pick the name
```

The first run prints a link to click if Funnel isn't enabled for your tailnet yet. Once it's up, `relayd` prints one line you paste to everyone:

```text
relayd public address: https://relay.tail1234.ts.net
```

Use that as `--scheduler` in `relay pool create` and the invite it prints is ready to share. The plain `:8080` listener stays up too, so machines on your LAN can keep using the local address. Tailscale state lives in `relay/tsnet` under your user config directory (`~/.config` on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows), so the address survives restarts. Funnel has a bandwidth cap, which is fine for heartbeats and job output (capped at 1 MB) but not for bulk data.
