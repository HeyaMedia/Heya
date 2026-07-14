# Remote access — UPnP, certificates, reachability

Plex-style direct remote access, production-only. The server maps a router
port via UPnP, serves the full app on its own TLS listener, gets real
certificates through ACME DNS-01 against a user-supplied DNS provider, and
verifies reachability outside-in through the heya.media connectivity-check
service. Everything lives in the single binary; heya.media's only role is
the probe (and, later, the paid `*.heya.direct` tier).

## Architecture

```
internal/remote/            the subsystem (no service-layer deps)
  remote.go                 Manager, state machine, maintenance loop
  upnp.go                   IGD discovery + port mapping (goupnp, IGDv2→v1)
  probe.go                  heya.media /v1/check + /v1/ip client (tcp4-pinned)
  dns.go                    libdns providers (deSEC/DuckDNS/Cloudflare),
                            zone-pinning adapter, lan./wan. record syncer
  certs.go                  certmagic (ACME DNS-01 wildcard) + persistent
                            self-signed fallback + the TLS listener
internal/service/remote_settings.go   DB-backed settings (system_settings
                            "remote.*"), env-lock provenance, port minting
internal/server/remote_huma.go         /api/remote/* + public probe echo
cmd/heya/cmd/remote.go      heya remote status|check|enable|disable
```

- **Production-only.** Under `--dev-backend` the manager is nil and the API
  reports it unavailable; the dev-proxy is a dumb reverse proxy with no
  network subsystems (same policy as Tailscale).
- **Two orthogonal tiers.** Certificates/hostnames work without any open
  port (LAN HTTPS via the `lan.` record); reachability works without any
  DNS provider (bare IP + self-signed, which native clients can pin). The
  probe deliberately skips cert verification and challenge-checks identity
  instead.

## State machine

`disabled → starting → mapping → probing → reachable | unreachable |
unverified | error`, streamed over the `remote.status` WS event and shown
on Settings → Network.

- `unverified` — mapping looks fine locally but heya.media couldn't be
  reached to prove it (e.g. the check service isn't deployed yet).
- CGNAT detection: router's UPnP WAN IP is RFC1918/RFC6598 or disagrees
  with the IP heya.media observes → the UI stops the user from debugging
  port forwards and points at Tailscale.
- UPnP failure is non-fatal: a manual port forward still probes fine.

The maintenance loop (15 min) re-leases the mapping (7200s lease, falling
back to permanent for routers that reject timed leases), watches for WAN IP
changes (→ resync `wan.` record + re-probe), and re-checks hourly.

## DNS providers

| Provider   | Hostnames                         | Notes |
| ---------- | --------------------------------- | ----- |
| deSEC      | `wan.` + `lan.` under the domain  | free, full zone API, PSL-listed |
| DuckDNS    | the domain itself (WAN only)      | free; single A record per domain, 60s TTL |
| Cloudflare | `wan.` + `lan.` under own domain  | scoped token (Zone.DNS:Edit); records stay DNS-only — CF's proxy can't forward high ports |

Certificates: one wildcard per server (`base` + `*.base`) via Let's Encrypt
DNS-01 (certmagic + libdns), renewed automatically. No port 80/443 is ever
needed — DNS-01 has no port requirement, which is the whole reason this
scheme works with both taken. `HEYA_REMOTE_ACME_CA` can point at LE staging
for testing.

**Zone pinning gotcha:** certmagic finds the DNS zone by SOA-walking, which
lands on `duckdns.org` for DuckDNS domains (no per-subdomain SOA). The
`zonePinned` adapter in dns.go rebases every record onto the configured
domain before it reaches the libdns provider — applied uniformly to all
providers.

## Config & provenance

Env vars (all optional; UI-editable fields follow env > db > default):

```
HEYA_REMOTE_ENABLED        =false
HEYA_REMOTE_PORT           =0        # 0 = mint a random 20000-59999 port once, persist it
HEYA_REMOTE_CHECK_URL      =https://heya.media    (env-only)
HEYA_REMOTE_CERT_DIR       ={HEYA_DATA_DIR}/remote (env-only)
HEYA_REMOTE_ACME_CA        =         # env-only; empty = LE production
HEYA_REMOTE_ACME_EMAIL     =
HEYA_REMOTE_DNS_PROVIDER   =         # desec | duckdns | cloudflare
HEYA_REMOTE_DNS_TOKEN      =         # never exposed via API
HEYA_REMOTE_DOMAIN         =
HEYA_REMOTE_SUBDOMAIN      =
```

The generated port is sticky on purpose — it lands in bookmarks and client
configs. The DNS token is write-only through the API (`token_set` flag
signals presence; empty on save keeps the stored one).

## heya.media contract

`POST /v1/check {port, challenge}` — probes the **request's source IP only**
(anti-SSRF), TLS with verification skipped, `GET /api/connectivity/probe`
expecting the challenge back. Distinct error codes (`timeout`,
`connection_refused`, `tls_handshake`, `challenge_mismatch`, `same_network`)
map to distinct UI messages. `GET /v1/ip` returns the caller's IP for
display + CGNAT detection. The media-server side serves the challenge only
while a check it initiated is in flight.

**Hairpin caveat:** when the check service egresses behind the *same
router* as the target server (e.g. heya.media hosted on the same LAN), its
probe of the WAN IP is a hairpin connection and fails on routers without
NAT loopback even though the port is genuinely open (verified 2026-07-15:
external vantage points connected fine while the co-located prober got
RST). The service should compare the target IP against its own public
egress IP and return `same_network` instead of a false negative; Heya maps
that to the `unverified` phase.

## Lifecycle notes

- Shutdown (`Close`) keeps the router mapping — restarts must not strand
  remote clients. Only explicit user Disable unmaps.
- Enable is idempotent teardown-and-rebuild; that's also the config-apply
  path. Enable/Disable/Recheck serialize on one mutex, so a disable issued
  mid-enable queues and wins.
- ACME issuance runs async after the listener is already up; the persistent
  self-signed cert (10y, stable fingerprint) serves until the real one
  lands in the certmagic cache.

## Rate-limit runway (for the future heya.direct tier)

LE renewals via ARI are exempt from all rate limits; the 50/week per
registered domain only gates *new* issuance. When a shared domain tier
ships: file the rate-limit adjustment form early, then PSL-list the domain.
