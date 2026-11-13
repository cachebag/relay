# Run a scheduler and manage a pool

The admin runs `relayd` somewhere pool members can reach, creates the pool, and hands out its join token. You need the `relay` CLI, which needs [uv](https://docs.astral.sh/uv/getting-started/installation/). See [SUBMIT.md](SUBMIT.md) for installing it.

Release assets are named `relayd-<os>-<arch>.tar.gz` (`.exe` on Windows), where `<os>-<arch>` is one of `linux-amd64`, `darwin-arm64`, `darwin-amd64`, `windows-amd64`. The tarball holds one executable file: `relayd`. The examples below use `linux-amd64`; swap in yours.

## Run the scheduler

With the CLI installed:

```sh
relay install              # downloads relayd and relay-agent for this machine
relay scheduler start      # runs relayd in the background on :8080
```

`start` keeps the server admin token that `relayd` prints on first start, so `relay pool create` and the dashboard use it without being asked. `relay scheduler status` shows the address and whether it's running, `relay scheduler stop` stops it, and the log is `relayd.log` in your relay config folder. Add `--public` for an address that works from any network; see [TAILSCALE.md](TAILSCALE.md).

Or run `relayd` by hand:

```sh
curl -L https://github.com/cachebag/relay/releases/latest/download/relayd-linux-amd64.tar.gz | tar xz
./relayd                  # listens on :8080; leave it running
```

The first time it starts, `relayd` prints a server admin token. Save it. It is the only thing that can create pools on this scheduler, so strangers can't make pools on your server once it is reachable from the internet. If you lose it, `./relayd --new-admin-token` prints a new one and exits. `relayd` keeps its database, `relay.db`, in the folder you start it from; `relay scheduler start` always uses the config folder.

## Keep it running

Install a per-user service to start the scheduler now, start it again after
reboot or login as described below, and retry if it exits:

```sh
relay scheduler start --service --public
relay scheduler status
```

Omit `--public` for a local-only scheduler. `--port` and `--name` work as with
ordinary `start`. Public mode uses the saved Tailscale auth key, or prompts for
one; an environment-supplied `TS_AUTHKEY` is also saved for the service. Use a
reusable key and complete the Funnel setup described below.

The service uses the same `relay.db`, Tailscale state, and `relayd.log` as a
normal scheduler start. If a normal scheduler is already running, stop it
before installing the service. Once installed, plain `start` does not start a
second copy. Plain `stop` stops the service temporarily, but the registration
remains for the next reboot or login. Use `start --service` to resume it with
its saved options. Explicit options that differ from the installed command
are rejected with the saved command and instructions to remove the service
first. To remove the registration, or to change its options, run:

```sh
relay scheduler stop --service
relay scheduler start --service --public  # optional: reinstall with new options
```

Removing the service does not delete the database, settings, or log.

- **Linux:** Relay installs `~/.config/systemd/user/relayd.service` (or under
  `XDG_CONFIG_HOME`) and enables user lingering so the service can run after
  reboot without a login. Enabling lingering may require authorization
  under the machine's policy. Check `systemctl --user status relayd.service`
  if it fails.
- **macOS:** Relay installs a LaunchAgent at
  `~/Library/LaunchAgents/relay.relayd.plist`, with `RunAtLoad` and `KeepAlive`.
  It runs as your account and starts when you log in. Install, start, stop,
  status, and removal use your `gui` launchd domain without `sudo`. Check
  `launchctl print gui/$(id -u)/relay.relayd` if it fails.
- **Windows:** Relay registers a Scheduled Task for your account, triggered
  when you log in, with task settings that restart it on failure. Use a
  normal terminal as the scheduler owner. The task runs the installed
  `relayd.exe` directly and writes to the same `relayd.log`. To upgrade, use
  plain `relay scheduler stop`, run `relay install`, then run
  `relay scheduler start --service` to use the new binary. The registration
  does not need to be reinstalled. Task Scheduler and `relayd.log` show
  failures.

On macOS and Windows, the scheduler needs the owner's login session after a
reboot. A dedicated server laptop can use the operating system's automatic
login setting if it must resume without someone signing in; anyone with
physical access may then be able to use that account. Relay does not change
this setting.

Relay saves a per-profile registration record in `relayd-service.json` in
the config folder. If that record is lost while the unit or plist remains,
the error names the registration file and explains how to unload and remove
it before reinstalling. Follow those instructions only for your own Relay
service.

Service registration or saved settings contain the Tailscale key; the log
can contain the initial admin token. Keep the service configuration, Relay
config directory, and logs private. The service cannot run while the machine
is asleep, powered off, or waiting for an encrypted disk to be unlocked.
Check `relay scheduler status` and the log if the public address doesn't
answer; network or Tailscale setup failures are retried.

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

If `relayd` runs with `--public`, the invitation uses its public
`https://….ts.net` address instead, even when you create the pool through
`localhost`, so there is nothing to replace. See
[Run relayd on the internet](#run-relayd-on-the-internet).

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
./relayd --public          # add --name <hostname> to pick the name
```

The first run prints a link to click if Funnel isn't enabled for your tailnet yet. Once it's up, `relayd` prints one line you paste to everyone:

```text
relayd public address: https://relay.tail1234.ts.net
```

Use that as `--scheduler` in `relay pool create` and the invite it prints is ready to share. The plain `:8080` listener stays up too, so machines on your LAN can keep using the local address. Tailscale state lives in `relay/tsnet` under your user config directory (`~/.config` on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows), so the address survives restarts. Funnel has a bandwidth cap, which is fine for heartbeats and job output (capped at 1 MB) but not for bulk data.

## Move the scheduler to another machine

Back up the database while `relayd` is running:

```sh
relay scheduler backup --to relay-11-12-2026.db
```

Copy that file to the new machine, install the CLI and binaries there, stop
any `relayd` already running there, restore the database into the config
folder, and start:

```sh
relay install
relay scheduler restore relay-11-12-2026.db
relay scheduler start
```

The restored database contains the pools, join tokens, pool admin-token hashes,
nodes, jobs, and credits. Keep the server admin token: the backup and the
restored database use the same token's hash, so the same server admin token
still works. Do not restore while `relayd` is running.
