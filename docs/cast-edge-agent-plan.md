# Future concept: Heya Cast Edge Agent

Status: idea only. Do not begin until the native server-side Chromecast, DLNA,
and Yamaha providers are working and their provider/media contracts are stable.

## Problem

A user may access somebody else's Heya server over Tailscale or the internet
while their speakers and TVs live only on the user's own home LAN. Server-side
mDNS cannot discover those receivers, and the remote receivers cannot fetch a
LAN-only URL from the Heya server. Relaying multicast across Tailscale is the
wrong abstraction and would still leave the media path unresolved.

## Proposed shape

Run a small cross-platform Heya agent on Windows, macOS, Linux, a NAS, or an
HTPC. The agent:

1. signs in as one Heya user/device and opens an outbound authenticated control
   connection to Heya (WebSocket or a similarly reconnectable stream);
2. discovers AirPlay, Google Cast, DLNA, Yamaha, and later WiiM receivers on its
   local interfaces;
3. publishes a capability-scoped inventory to Heya, tagged with the owning user
   and agent ID;
4. accepts typed playback/control jobs for those discovered device IDs; and
5. proxies scoped Heya media to receiver-facing URLs on the local LAN.

The Heya server never needs an inbound connection to the user's home. Tailscale
is a good transport when available, but the protocol should work over ordinary
TLS as well.

```text
remote Heya ── authenticated media/control ──▶ user's edge agent
                                                   │
                      local discovery + playback  │
                                                   ▼
                                      AirPlay / Cast / DLNA receiver
```

For pull protocols, the receiver gets a short-lived URL hosted by the agent.
The agent validates that local token and range-proxies the matching media from
Heya using a separate upstream token. For push protocols such as the current
AirPlay path, the sender process runs beside the receiver in the agent and
consumes the Heya stream remotely. Do not expose the user's normal Heya bearer
token to a receiver.

The same agent could optionally be a playback endpoint itself. A desktop/HTPC
build may launch an embedded player such as mpv; a headless build can expose
only discovery/proxy capabilities. This should be a declared capability, not a
separate one-off protocol.

## Relationship to HeyaConnect

Do not build a second incompatible remote-device model. Reuse HeyaConnect's
device identity, ownership, liveness, and command/event envelope where it fits,
then add explicit capabilities such as:

- `cast.discovery.airplay`, `cast.discovery.googlecast`, `cast.discovery.dlna`
- `cast.proxy.pull_url`, `cast.sender.airplay`
- `playback.local.audio`, `playback.local.video`

An agent may expose several household receivers plus itself. Receiver IDs must
include the agent identity and the protocol's stable hardware ID so two agents
hearing the same TV do not silently collide.

## Security boundaries

This is the hardest part, not discovery.

- Inventory and commands are user-scoped. A server admin must not accidentally
  make one friend's living-room devices visible to every server user.
- The server sends typed commands against an agent-reported device ID. It must
  never gain a generic "connect to arbitrary LAN host/port" primitive; that
  would turn the agent into an SSRF and internal-network scanning service.
- Media jobs bind user, agent, receiver, exact media resource, and expiry.
  Revocation or agent logout invalidates future fetches.
- The agent accepts media only from its configured Heya origin and follows a
  conservative redirect policy. Receiver callbacks are limited to the
  negotiated session.
- Agent updates need signed artifacts and a clear protocol-version/capability
  negotiation story. A long-lived home daemon is a meaningful trust anchor.
- Logs and diagnostics must redact tokens and avoid leaking another user's LAN
  addresses to admins/users who do not own that agent.

## Operational critique

- "Tiny client" can easily become a remote-execution platform. Keep a small,
  versioned command schema and resist arbitrary subprocess/shell hooks.
- The WAN link becomes part of playback: Heya → agent → receiver. Seeking needs
  correct Range proxying; lossless audio and high-bitrate video need bandwidth
  checks, backpressure, and probably an agent-side read-ahead cache.
- Transcoding belongs at Heya initially so the agent remains small, but that
  spends upstream bandwidth. Optional agent-side transcoding is a much later
  capability with substantial binary/CPU complexity.
- AirPlay timing and multicast discovery run on the agent's LAN, which is good;
  command latency over the WAN is less important than putting the actual sender
  beside the receiver.
- Sleep, laptop roaming, interface changes, firewalls, and macOS/Windows local
  network permissions make liveness noisy. The server must expire inventory
  quickly and present "agent offline" separately from "receiver offline".
- Multiple agents in one home will discover duplicate receivers. Prefer one
  active route using freshness/reachability and let the user choose an agent
  when ambiguity remains.

## Sensible future milestones

1. Headless agent skeleton: enrollment, outbound liveness, per-user inventory.
2. Google Cast discovery plus scoped HTTP Range proxy (smallest end-to-end pull
   proof once the server provider exists).
3. AirPlay sender running on the agent against an upstream Heya audio stream.
4. DLNA/Yamaha capabilities using the same proxy and inventory model.
5. Optional local audio/video endpoint (mpv or a purpose-built player).
6. Packaging, signed auto-update, service install, and deep diagnostics for the
   four desktop/server operating-system families.
