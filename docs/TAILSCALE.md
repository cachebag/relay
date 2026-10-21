# Put your scheduler on the internet

`relayd --public` gives your scheduler a public `https://` address through Tailscale. Contributors anywhere can join your pool, and they install nothing extra. You don't open ports, buy a domain, or set up certificates. Only you need a Tailscale account, and the free plan is enough.

Five minutes the first time. One command after that.

## 1. Make a Tailscale account

Sign up at https://login.tailscale.com with GitHub, Google, or any other option. You don't need the Tailscale app for this. Your tailnet gets a name like `taild0efc0.ts.net`, and your scheduler's address will be `https://relay.<that name>`.

## 2. Check DNS settings

In the admin console, click **DNS** in the sidebar (1) and scroll down. **MagicDNS** (2) and **HTTPS Certificates** (3) both need to be on. If a button says "Disable", that one is already on. If it says "Enable", click it.

![DNS page: MagicDNS and HTTPS Certificates both enabled](img/tailscale-dns.png)

## 3. Make an auth key

Click **Keys** in the sidebar (1), then **Generate auth key**. In the dialog:

- **Reusable** on (2). Without it the key works once and `relayd` can't restart.
- **Ephemeral** off (3). Otherwise your scheduler's address changes every run.
- Leave expiry at 90 days and Tags off. Click **Generate key** (4).

![Generate auth key dialog: Reusable on, Ephemeral off](img/tailscale-auth-key.png)

Copy the key. It starts with `tskey-auth-` and is shown only once. Treat it like a password: anyone with it can add machines to your tailnet.

## 4. Run relayd

Set the key and start the scheduler.

macOS and Linux:

```sh
export TS_AUTHKEY=tskey-auth-...
./relayd --public
```

Windows PowerShell:

```powershell
$env:TS_AUTHKEY="tskey-auth-..."
.\relayd.exe --public
```

The first start prints the server admin token (save it), a few Tailscale lines, then the one you need:

```text
relayd public address: https://relay.taild0efc0.ts.net
```

If the first run instead prints a link about enabling Funnel, open it, click **Enable**, and run `relayd --public` again. That's a one-time switch for your tailnet.

Back in the admin console, **Machines** now lists a node named `relay` with a Funnel badge. That's your scheduler. Leave it there; it's what keeps the address the same between runs.

![Machines page: the relay node with a Funnel badge](img/tailscale-machines.png)

## 5. Make a pool and invite people

Use the public address as the scheduler:

```sh
relay --scheduler https://relay.taild0efc0.ts.net pool create my-pool --admin-token <server admin token>
```

The last line it prints is the exact command contributors run. Send it to them:

```text
relay-agent --scheduler https://relay.taild0efc0.ts.net --pool-token <join token>
```

Log in yourself so every other command uses the public address too:

```sh
relay login --scheduler https://relay.taild0efc0.ts.net --pool-token <join token>
relay nodes
```

## Good to know

- `--name` picks the hostname. `relayd --public --name lab` gets `https://lab.<tailnet>.ts.net`.
- The plain `:8080` listener stays up, so machines on your LAN can keep using `http://<your-ip>:8080`.
- Tailscale state lives in `relay/tsnet` under your user config directory. Delete that folder to start over with a fresh node.
- Funnel has a bandwidth cap. Fine for heartbeats and job output, which is capped at 1 MB. Not for moving big files.
- Without `TS_AUTHKEY`, `relayd --public` exits with code 2 and says what to set.
