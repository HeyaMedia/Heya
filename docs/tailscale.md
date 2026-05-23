# Tailscale (tsnet) integration

Heya can join your tailnet directly via [tsnet](https://tailscale.com/docs/features/tsnet) — no host-level `tailscaled` needed and no port forwarding. The binary becomes its own tailnet node with its own hostname/IP, and the API + SPA are reachable over the tailnet on the side of the LAN listener.

## Quick start

1. Generate a [reusable auth key](https://login.tailscale.com/admin/settings/keys) (or skip and do interactive login on first run).
2. Add a `tailscale:` block to `heya.yaml`:

   ```yaml
   tailscale:
     enabled: true
     hostname: heya       # appears in your tailnet admin console
     https: true          # serve TLS via Tailscale-issued cert
     funnel: false        # public-internet exposure (opt-in)
   ```

3. Export the auth key (preferred over committing it to YAML):

   ```bash
   export HEYA_TAILSCALE_AUTHKEY=tskey-auth-xxx
   ```

4. Start the server. The first start prints a login URL if no auth key is set:

   ```bash
   ./bin/heya serve
   # → tailscale node up   hostname=heya magic_dns=heya.tail-scale.ts.net …
   ```

5. From any tailnet device, hit `https://heya.tail-scale.ts.net` (replace with your tailnet's MagicDNS suffix). The same Heya UI you serve on `:8080` answers there.

## How it slots into `serve`

`heya serve` starts the LAN listener on `${HOST}:${PORT}` (default `0.0.0.0:8080`) the way it always has. If `tailscale.enabled: true`, it *additionally* opens listeners on the tailnet:

| Mode               | Tailnet bindings                              |
| ------------------ | --------------------------------------------- |
| `https: false`     | tailnet `:80` → same handler                  |
| `https: true`      | tailnet `:443` (Tailscale cert) + `:80` redirector |
| `funnel: true`     | tailnet+public `:443` (Funnel) + `:80` redirector  |

All bindings share the same `http.Handler`, so every route — REST, the embedded SPA, the WebSocket event stream, HLS segments — works over both transports automatically.

The LAN listener never goes away. If tsnet onboarding fails (no internet, bad auth key, tailnet admin paused you, etc.) the LAN listener keeps serving — tsnet is purely additive.

## HTTPS

`https: true` (the default when Tailscale is enabled) uses Tailscale's built-in cert authority. The cert is issued for the node's MagicDNS name (e.g. `heya.tail-scale.ts.net`) and renewed automatically.

Prereq: HTTPS must be enabled for your tailnet — one-time toggle at <https://login.tailscale.com/admin/dns/https>. Without it, `ListenTLS` fails and Heya falls back to plain HTTP on tailnet :80.

## Funnel (public exposure)

Funnel lets anyone on the public internet reach Heya at the same MagicDNS name. Useful for "share with friends/family" without needing them to install Tailscale.

- Off by default.
- Toggleable from **Settings → Tailscale** in the UI, or via `POST /api/tailscale/funnel {"enabled": true}`.
- Heya's authentication still applies — Funnel only changes the transport, not who can access.
- Requires Funnel to be enabled for your tailnet in the admin console.

## State directory

tsnet stores the node identity (machine key, cert cache, etc.) under `data/tailscale/` by default. The directory is mode `0700`. Wipe it (or `heya tailscale logout`) to force re-onboarding under a different identity.

## CLI

```bash
heya tailscale status         # show current node state
heya tailscale status --json  # machine-readable
heya tailscale logout         # wipe local identity (re-onboard next start)
```

Run while the main `heya serve` is **not** running — both processes would race for the same state directory.

## Event bus

Heya emits a `tailscale.status` event on the WebSocket event stream every time the node state changes (backend transitions, IP assignments, login URL appearing, Funnel flipped). Subscribers see the same `Status` JSON the REST endpoint returns.

## Config surface

| YAML key                  | Env var                       | Default | Notes                                                       |
| ------------------------- | ----------------------------- | ------- | ----------------------------------------------------------- |
| `tailscale.enabled`       | `HEYA_TAILSCALE_ENABLED`      | `false` | Master switch                                               |
| `tailscale.hostname`      | `HEYA_TAILSCALE_HOSTNAME`     | `heya`  | Node name as shown in tailnet admin                         |
| —                         | `HEYA_TAILSCALE_AUTHKEY`      | —       | Auth key; preferred location since it's a secret            |
| `tailscale.state_dir`     | `HEYA_TAILSCALE_STATE_DIR`    | `data/tailscale` | Where tsnet writes its identity                    |
| `tailscale.https`         | `HEYA_TAILSCALE_HTTPS`        | `true`  | Bind tailnet :443 with a Tailscale-issued cert              |
| `tailscale.funnel`        | `HEYA_TAILSCALE_FUNNEL`       | `false` | Expose on the public internet via Funnel                    |

Env vars override the YAML; missing values fall back to defaults.
