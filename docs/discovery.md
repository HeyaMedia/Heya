# LAN discovery (mDNS / DNS-SD)

Heya announces itself on the local network so first-party clients —
HeyaClient, HeyaTV — can find the server instead of asking the user to type a
URL. It is on by default.

The announcement is a LAN-scoped multicast broadcast. It opens no port, grants
no access, and every endpoint a client subsequently calls is authenticated
exactly as before. What it publishes is a name, an address, a port and a small
TXT record.

| | |
| --- | --- |
| Package | `internal/discovery` |
| Owner process | `heya serve` (not the worker — one announcement per server) |
| Available in dev | **Yes**, unlike Tailscale/remote access |
| Settings UI | Settings → Network → *LAN discovery* |
| CLI | `heya discovery status` / `enable` / `disable` / `name` / `browse` |
| API | `GET/PUT /api/discovery/{status,config}` (admin), `GET /api/server/info` (public) |

## What is on the wire

One DNS-SD service instance:

```
<instance>._heya._tcp.local.    PTR  → the instance
                                SRV  → heya-<host>.local. : <port>
                                TXT  → the record below
heya-<host>.local.              A/AAAA → the server's addresses
```

The TXT record is the client contract. It is versioned: **a client MUST check
`v` and ignore instances whose version it does not understand.** New keys may
be added within a version; existing keys will not change meaning.

| Key | Example | Meaning |
| --- | --- | --- |
| `v` | `1` | Record schema version |
| `id` | `772e414f…` (32 hex) | Stable per-install id. Key saved-server entries on this — it survives renames, address changes, and LAN↔remote switches |
| `name` | `knas` | Display name |
| `ver` | `v0.4.132` | Heya build version. Carries the leading `v` from the release tag — strip it before comparing versions |
| `scheme` | `https` | How to talk to the port in the SRV record |
| `path` | `/` | Base path of the web app |
| `api` | `/api` | Base path of the JSON API |
| `remote` | `https://wan.heya.example.com:41234` | Public origin for off-LAN access. Present only when direct remote access is configured — a client provisioned on the LAN can store it and keep working from outside |

## Client flow

1. **Browse** `_heya._tcp` in `local.`.
2. **Build the origin** from the SRV answer and `scheme`. Prefer a literal
   A-record address over the `.local.` name — Android has no mDNS resolver, so
   a hostname is useless there. Heya orders the published addresses so the
   server's default-route address comes first and tailnet/CGNAT addresses last;
   try them in order.
3. **Confirm** with an unauthenticated `GET /api/server/info`:

   ```json
   {
     "product": "heya",
     "id": "772e414f2945d53bd3fe797c2a1b059b",
     "name": "knas",
     "version": "v0.4.132",
     "api_base_path": "/api",
     "service_type": "_heya._tcp"
   }
   ```

   Reject anything where `product != "heya"` — that is how you tell a real
   server from something else that happens to answer on that port. Nothing in
   this response is sensitive; it is all already in the TXT record.
4. **Offer it to the user**, then run the normal login flow.

`heya discovery browse` performs exactly steps 1–2 and prints the result, so
it is the reference for what a client should see.

## Configuration

Standard provenance: env wins and locks the UI, DB values overlay defaults.

| Variable | Default | UI-editable | Notes |
| --- | --- | --- | --- |
| `HEYA_DISCOVERY_ENABLED` | `true` | yes | |
| `HEYA_DISCOVERY_NAME` | *(empty)* | yes | Empty = this machine's hostname. Must be unique on the network |
| `HEYA_DISCOVERY_PORT` | `0` | no | `0` = the port the server binds. Set it when a reverse proxy or container port map means clients must connect elsewhere |
| `HEYA_DISCOVERY_HOST` | *(empty)* | no | Hostname to advertise under `.local`. Only the first label is used |
| `HEYA_DISCOVERY_ADDRESSES` | *(empty)* | no | Comma-separated IPs to publish instead of the ones enumerated locally |
| `HEYA_DISCOVERY_INTERFACES` | *(empty)* | no | Comma-separated NIC allowlist. Empty = every multicast-capable interface |

## When it doesn't work

mDNS is confined to one layer-2 segment. It does not cross subnets, most VPNs,
or a router without an mDNS reflector. Before debugging Heya, confirm with
`heya discovery browse` **from the client's network** — the server-side
`heya discovery status` only proves the announcement was published locally,
never that anything received it.

**Containers are the common failure.** In bridged Docker networking or a
Kubernetes pod, multicast does not reach the LAN and the container's own IP is
not routable from it, so the default behaviour publishes an address no client
can use. Two options:

- **Host networking** (`--network host`, or `hostNetwork: true`) — the
  announcement then behaves exactly as it would on the host.
- **Advertise the host explicitly** — keep bridged networking and set
  `HEYA_DISCOVERY_ADDRESSES` to the host's LAN IP and `HEYA_DISCOVERY_PORT` to
  the published port. Multicast still has to escape the container for this to
  help, so host networking is the reliable answer.

Heya's own production deployment is a Kubernetes pod and already runs with
`hostNetwork: true`, so it is discoverable as-is: the serve pod holds the
node's LAN address and announces under the node's hostname.

**A multi-bridge host publishes dead addresses.** That node enumerates 64
multicast-capable interfaces, and the announcement carries every non-loopback
address it finds:

```
ipv4: 192.168.10.10   ← default route, ordered first, the one clients use
ipv4: 172.17.0.1      ← docker bridge, unroutable from the LAN
ipv4: 10.0.0.210      ← CNI, unroutable from the LAN
```

The ordering means a client connects on the first try, so this is cosmetic
rather than broken. Pin `HEYA_DISCOVERY_INTERFACES` to the real LAN NIC if you
want the trailing two gone — a client that ever does fall through to them
waits out a connect timeout each.

## Implementation notes

- **Never advertise under the machine's own hostname.** Heya runs its own mDNS
  responder in-process, and most hosts already run one (mDNSResponder on
  macOS, avahi on Linux) that owns *and defends* `<hostname>.local`.
  Publishing our own A records for that name is a conflicting authoritative
  answer, so RFC 6762 conflict resolution fires and **the OS renames the
  machine** — `kWorkBookPro` → `kWorkBookPro-2` → `-3`, once per start. This
  actually happened during development. Records therefore go out under
  `heya-<host>.local`, which nothing else claims; clients dial the A record's
  address, not the name, so they never notice. An explicit
  `HEYA_DISCOVERY_HOST` is used verbatim — name it by hand and you own the
  consequences. See `TestAdvertisedHostLabelNeverClaimsTheMachinesOwnName`.
- **Always DNS-SD proxy registration.** `zeroconf.Register` derives the host
  name from `os.Hostname()` and appends `.local.` only when it thinks the
  suffix is missing. On a machine already called `mac.local` that produced
  `mac.local.local.`; passing an untrimmed domain instead produced a name with
  no trailing dot, which `miekg/dns` refuses to pack — so *nothing* was
  answered at all, silently. `RegisterProxy` with a bare host label and
  explicit addresses removes the guess, and is the same code path containers
  need. See `TestHostLabel`.
- **The advertiser runs in dev.** Tailscale and remote access are
  production-only because they expose the server; this does not, and pointing a
  real client at `heya serve --dev-backend` is the reason it exists.
- **Passive mode never writes.** `LoadDiscoveryFromDB` is skipped (a borrowed
  production DB carries production's name, and republishing it would put two
  servers on the LAN claiming one instance name), config writes return 403, and
  `ServerID` falls back to a value derived from the hostname rather than
  minting one into someone else's database.
- **The TXT record re-publishes when the remote URL appears.** Remote access
  finishes its bring-up asynchronously, so `serve` re-applies the
  advertisement when `RemoteURL` changes.
- **Goodbye packets on shutdown.** Teardown withdraws the record before the
  process exits so clients drop it immediately instead of offering a dead
  entry until the TTL expires.

## Testing

```bash
# Unit tests (TXT format, host labels, address ordering)
go test ./internal/discovery/ -timeout 120s

# Real multicast round-trip: register, browse, assert the record came back.
# Skipped by default — needs a multicast interface and, on macOS, triggers
# the local-network permission prompt.
HEYA_DISCOVERY_LIVE_TEST=1 go test ./internal/discovery/ -run TestLive -v -timeout 60s

# What a client sees, from this machine
heya discovery browse --wait 6s
heya discovery browse --json | jq
```
