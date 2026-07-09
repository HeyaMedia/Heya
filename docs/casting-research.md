# Casting / remote-playback research (2026-07)

Research pass on the FUTURE.md "DLNA / Chromecast / AirPlay" line, scoped to
the server-side model: **Heya's server streams to (or controls) the receiver;
web clients only send control commands to Heya**. Same architecture as Music
Assistant, OwnTone, and Jellyfin "Play To" — no browser-side Cast SDK.

Findings below were produced by a fan-out research run (25 sources, 124
extracted claims, top 25 adversarially verified 3-vote, 25/25 confirmed,
0 refuted). Confidence labels reflect that verification.

## TL;DR

| Protocol | Verdict | Effort in Go | Notes |
| --- | --- | --- | --- |
| **AirPlay 1 (RAOP) sender** | ✅ Viable — best coverage/effort | Moderate (subprocess pattern proven) | AirPlay-2-branded receivers still accept RAOP senders; MA + OwnTone default to it |
| **AirPlay 2 sender** | 🟡 Hard but open | High | Pairing layer solved (pair_ap, MIT; SRP exists in Go); buffered-stream+PTP layer mature only in OwnTone. Fallback-only |
| **Google Cast v2** | ✅ Viable — protocol alive & open | Small (pure Go, proven twice) | Hardware discontinued ≠ protocol dead; still fully open to third-party senders mid-2026 |
| **DLNA / UPnP AVTransport** | 🟡 Viable, quirky | Moderate | go2tv proves it in Go; gapless device-dependent; Jellyfin demoted it to a plugin |
| **WiiM HTTP API** | ✅ Verified, near-free | Trivial | Official `setPlayerCmd:play:url` plays arbitrary stream URLs |
| **HEOS / BluOS / Yamaha YXC** | 🟡 Promising, unverified | Small each | Local unauthenticated APIs extracted from vendor PDFs but didn't make the verify cut |
| **Roon RAAT / Spotify Connect / Tidal Connect** | ❌ Closed to third-party senders | — | No public sender path (presumed; not formally verified) |

**Recommended pairing:** RAOP sender first (covers the whole
AirPlay-2-branded hi-fi receiver class in one protocol), Google Cast v2
second (pure-Go, URL-pull, cheap). DLNA third. Vendor APIs as opportunistic
extras.

## 1. AirPlay 1 / RAOP — the winner

The critical verified fact: **you don't need AirPlay 2 to reach AirPlay 2
receivers.** AP2 devices are backwards compatible with RAOP senders.

- Music Assistant's AirPlay provider defaults to "Automatically select"
  which resolves to **AirPlay 1 (RAOP) for most devices**, using AP2 only
  for a small exception list of known-broken-RAOP devices (e.g. Ubiquiti).
  ([MA docs](https://www.music-assistant.io/player-support/airplay/))
- OwnTone likewise speaks RTSP-ANNOUNCE (RAOP) to AirPlay-2-branded
  receivers by default; log line "Ignoring type AirPlay 2 for device …,
  will use type AirPlay 1".
- **No sender-side MFi/FairPlay licensing gate.** Receivers advertising
  `et=4` may require a `/auth-setup` step, but the open-source solution
  (OwnTone `raop.c`, copied by pyatv) is a fire-and-ignore Curve25519
  pubkey exchange — "We never verify anything."
- Gotcha: HomeKit-managed speakers can 400 the ANNOUNCE until Home app
  access is set to "Anyone On the Same Network" — onboarding docs point.

### Proven implementation path

[philippe44/libraop](https://github.com/philippe44/libraop) (C, RAOP v2
with sync, cross-platform, actively pushed as of 2026-05-07) ships
**cliraop**, a CLI player that accepts raw PCM on stdin. Music Assistant's
production architecture is exactly: spawn `cliraop … -` and pipe
ffmpeg-decoded PCM into stdin, control over named pipes. MA explicitly
chose this because "Python is not suitable for real-time audio streaming" —
Go arguably is, but the subprocess path is the zero-risk starting point via
`os/exec`, mirroring Heya's existing ffmpeg usage.

- Discovery: mDNS `_raop._tcp` (+ `_airplay._tcp`).
- ⚠️ libraop's repo has **no LICENSE file** — redistribution diligence
  needed before bundling binaries (MA redistributes prebuilt binaries,
  suggesting but not proving permissive terms).
- No pure-Go RAOP *sender* library was found to exist. A clean-room Go port
  is plausible later (RTSP + RTP + AES-CBC + ALAC framing, auth solved,
  reference implementations in C and Python) but is not day-one work.

## 2. AirPlay 2 sender — hard, open, mostly unnecessary

- **Pairing: solved.** AP2 auth is HomeKit/HAP SRP over documented HTTP
  endpoints (`/pair-setup`, `/pair-verify`).
  [pair_ap](https://github.com/ejurgensen/pair_ap) (MIT, by the OwnTone
  author) implements client+server; many receivers accept **transient
  pairing with hardcoded PIN 3939** (non-interactive, headless-friendly).
  The same SRP handshake already exists in Go (brutella/hap).
- **Streaming: the hard part.** Buffered-audio + PTP timing is mature only
  in OwnTone (AP2 PTP default in 29.x, 2026). pyatv's public AP2 docs cover
  remote-control setup only; audio-streaming sections are literally "TBD".
  Even Music Assistant didn't reimplement it — it wraps an OwnTone-derived
  `cliap2` subprocess.

### cliap2 / music-assistant/cliairplay (checked 2026-07-09)

[music-assistant/cliairplay](https://github.com/music-assistant/cliairplay)
is OwnTone's AirPlay 2 output code carved out into a standalone CLI binary
(`cliap2`) — "Based on owntone", **GPL-2.0** (unlike libraop, it actually
has a license), maintained by the MA team, with CI-built binaries for
Linux x86_64/arm64 and macOS Intel/Apple Silicon (exactly Heya's platform
set). Same subprocess pattern as cliraop: spawn per playback, feed PCM,
control via pipes. **PTP timing support merged 2026-03-16 (PR #98)** —
closing the Shairport-receiver/timing gap the MA docs still described.
Known remaining gap: password-protected receivers (no password pairing).

**Verdict:** cliraop (AirPlay 1) + cliap2 (AirPlay 2) as sibling
subprocess backends give the full OwnTone AirPlay capability without
running the OwnTone daemon. RAOP remains the default; cliap2 handles
RAOP-broken devices and is the upgrade path.

### Validated live 2026-07-09 — cliap2 → Yamaha RX-V6A ✅

CI artifact `cliap2-macos-arm64` (run 25268546639, v1.5) streamed test
tones and then a full song (FLAC-in-m4a, ffmpeg-decoded) to the house
receiver ("Anlæg", RX-V6A, AirPlay 2 fw p20.1.70, `192.168.1.216:7000`).
Transient pairing → encrypted session → UDP ALAC stream → audible music,
zero Heya code. The receiver woke from network standby when the session
landed.

Working recipe (v1.5 specifics, some learned the hard way):

- Audio is **stdin only** (`--pipe` is accepted but obsolete). Format:
  s16le 44.1 kHz stereo by default (config keys `mass { pcm_sample_rate,
  pcm_bits_per_sample }` allow 48/88.2/96 kHz and 16/24/32-bit). ffmpeg
  feed: `ffmpeg -i track -vn -f s16le -ar 44100 -ac 2 -`.
- Mandatory args: `--name --hostname --address --port --txt
  --command_pipe` (the command FIFO is auto-created; commands + metadata
  go through it).
- ⚠️ **`--txt` must be the `_airplay._tcp` TXT record** (has `deviceid=`,
  `features=`, `model=`, `pk=`), NOT the `_raop._tcp` one (`et=`, `ft=`,
  `am=`). One argument of space-separated `"k=v"` pairs, verbatim from
  `dns-sd -L <name> _airplay._tcp local.`.
- ⚠️ **Silent failure mode**: with a bad/missing `deviceid`, cliap2 logs
  one `AirPlay device '<name>' is missing a device ID` line, then its
  internal player still consumes stdin and reports "playing" with
  advancing position — to nowhere, exit 0. The only trustworthy success
  marker is the stderr line MA watches for:
  `Callback from AirPlay 2 device <name> to device_activate_cb (status 2)`.
- `--ntpstart` = `cliap2 --ntp` output (raw 64-bit NTP, seconds in the
  high 32 bits) + lead. 4 s is too tight (warns and trims ~0.8 s of
  audio: 2.5 s establishment + ~4.5 s stdin priming); **7 s worked
  cleanly**. Note: uint64 NTP overflows shell signed arithmetic — compute
  the sum outside `$(( ))`.
- `--volume` is AirPlay device volume 0–100; 30 → `SET_PARAMETER` −21 dB
  on the wire. On the RX-V6A that made a −30 dBFS tone quietly audible
  and a −10 dB-attenuated song comfortable.
- Transient pairing worked against the Yamaha with no `--auth`/PIN
  (status flags: no password, no PIN, no one-time pairing).
- "PTP daemon unavailable, only NTP will be available" — PTP wants the
  privileged ports; NTP timing was fine for single-device playback.
- macOS binary needs brew dylibs: ffmpeg, libplist, confuse, zlib,
  libiconv, json-c, libevent, libsodium, libgcrypt, libxml2, libunistring
  (most arrive with `brew install ffmpeg`). Linux CI builds target Debian
  Bookworm shared libs — fits the Heya container base.
- Progress/state parsing = stderr line matching (MA does the same:
  "Starting at", "Pause at", "end of stream reached", "put delay
  detected" for buffer underrun).

**Command-pipe protocol** (the `--command_pipe` FIFO; newline-delimited
`KEY=value`, per `mass.c`):

- Metadata: `TITLE=` `ARTIST=` `ALBUM=` `DURATION=<sec>` `ARTWORK=<url>`
  accumulate as *partial* state — nothing is sent to the device until an
  **`ACTION=SENDMETA`** line arrives, which flushes the bundle as
  `SET_PARAMETER` (text/progress/artwork) to the receiver. (Tested live:
  metadata without SENDMETA parses but never displays.)
- Position: `PROGRESS=<sec>` updates the device progress bar.
- Control: `ACTION=PAUSE` / `ACTION=PLAY` / `ACTION=STOP`, and live
  `VOLUME=<0-100>`. `PIN=<4 digits>` answers a PIN-pairing challenge.
- ⚠️ **`ACTION=PAUSE` pauses stdin *intake*, not playback** (live-tested
  2026-07-10): the player keeps draining whatever is already buffered.
  With an unthrottled feeder the whole track is inside cliap2 within
  seconds and pause/resume are no-ops ("Command received to PLAY, but
  current state is playing"). With a real-time feeder (`ffmpeg -re`) the
  standing buffer ≈ the pre-roll priming (~4-5 s at a 7 s NTP lead), so
  pause takes effect ~4 s late via a clean starvation suspend ("Source
  is not providing sufficient data, temporarily suspending playback" →
  `pb_suspend`). Heya's session therefore implements pause as
  freeze-position + transport stop (instant silence) and resume as
  respawn-at-position — the FIFO pause is unused for now.
- The RX-V6A front panel shows only "AirPlay" until metadata is flushed;
  richer displays (MusicCast app, TVs) render title/artist/artwork.
- **Receiver→sender backchannel: not implemented in cliap2 v1.5** (live
  test 2026-07-10: user worked the RX-V6A's volume knob and play/pause
  buttons through a full run with `--dacp_id` set and `--loglevel 5` —
  zero DACP callbacks, zero event-channel messages, stream volume state
  never moved). `--dacp_id` only stamps the DACP-ID/Active-Remote RTSP
  headers; cliap2 runs no DACP listener and ignores event-channel
  payloads (only logs disconnects). Consequences and options:
  - The AVR's physical knob is amp gain *downstream* of the AirPlay
    decode — independent of stream volume; it works, it just can't
    mirror into the Heya UI via this path.
  - **v1: ship one-way control** — the server is the source of truth,
    Heya UI is the remote. This matches how the session model works
    anyway.
  - **v2 option (pure Go, no cliap2 change):** the provider itself can
    advertise `_dacp._tcp` in mDNS under the DACP-ID we already pass and
    run a tiny HTTP listener for `/ctrl-int/1/*` commands — that's the
    standard remote-control path receivers aim at. Needs a live test to
    see if the Yamaha sends anything at all.
  - **v2 option for MusicCast devices:** the YXC local HTTP API has
    status polling + UDP event subscription — poll/subscribe for real
    amp volume + transport state and mirror into the session. Vendor
    -specific but covers this exact receiver.

**Commence-failure ROOT CAUSE (runs 5–8; solved by Sonnet debug agent
same evening, confirmed live 2/2)** — two composed C bugs in cliap2
v1.5, *not* receiver state, not NTP, not metadata:

- **Bug A** (`cliap2.c`): `input_write_ms` gets a sane default of 15 ms
  at line 591, then is unconditionally clobbered to 0 at line 752 by a
  local that's only non-zero if the hidden `--input_write_ms` flag was
  passed. `latency_ms` is likewise 0 unless `--latency` is passed.
- **Bug B** (`mass.c:1780`): the pre-roll commence gate compares signed
  `delta_ms` (int64) against `latency_ms + input_write_ms` where
  `latency_ms` is **uint64** — C promotes the comparison to unsigned, so
  the moment `delta_ms` goes negative it reads as a huge positive value
  and the "too early, keep waiting" branch is *permanently* true. The
  player loops `input_wait()` forever: no start-sync packet, no
  `event_play_start`, no keep-alives (they start with playback), and the
  receiver times out the idle RTSP session at ~+31 s.
- **Combined:** with `--latency` omitted the escape window is 0 ms wide
  against a 10 ms input-poll grid — success is a per-run coin flip on
  sub-10 ms session-establishment jitter. Explains runs 2–4 (lucky) vs
  5–8 (wedged) exactly. MA always passes `--latency`, which is why this
  never surfaced upstream.
- **THE FIX: always pass `--latency N`** (any N ≥ 0; +250 ms DAC floor
  is added internally). Confirmed: 2/2 clean full-cycle runs against the
  RX-V6A with `--latency 100`, plus a full song with live metadata
  display, plus the worst case — `ACTION=STOP` mid-song → 2 s gap →
  immediate new session → instant commencement (2026-07-10). Rapid
  reconnects are safe with the fix; no upstream report per user's call
  (keep the workaround local).
- **Sharp edge for short clips:** an unthrottled ffmpeg can dump a short
  PCM payload into the pipe and hit EOF *before* the pre-roll wait ends
  → "end of stream reached" with no audio ever sent. Pace short/test
  clips with `ffmpeg -re`; real tracks are long enough not to care.
- Upstream issue draft (title, line refs, repro, fix suggestions) is in
  the debug agent's report; not yet filed.

**Supervision rules for the Heya provider** (full failure-mode catalog
with signatures/timeouts in `$SCRATCHPAD/failure_investigation.md`, key
rules here):

- Always generate `--latency` in the invocation (root fix). Defense in
  depth: if `event_play_start` hasn't appeared within
  `ntp_lead + session_establishment_latency + 2 s`, SIGTERM (never
  SIGKILL — TEARDOWN must go out) and retry; absolute kill ceiling ~25 s
  so we beat the receiver's ~31 s idle timeout.
- stderr is the source of truth: `device_activate_cb (status 2)` =
  connected, `event_play_start` = actually streaming. Internal "playing"
  status alone is meaningless (the player happily plays to nowhere).
- Only write the command FIFO after `event_play_start` (pre-roll writes
  wedged the session thread in run 5; mid-playback writes are safe).
- `ntpstart time too soon` WARN is non-fatal (audio start gets trimmed);
  supervisor should bump the lead by the reported amount, not fail.
- Pairing/auth `[ LOG]` errors (`pair`, `verify`, `Ciphering`) are hard
  failures — surface to user, don't retry blindly.

## 3. Google Cast — not dead, still open

Chromecast *hardware* was discontinued; the **Cast v2 protocol** (mDNS
`_googlecast._tcp` + length-prefixed protobuf over TLS :8009) survives in
TVs, speakers, and the Google TV Streamer, and remains fully open to
third-party senders as of mid-2026:

- pychromecast (Home Assistant's production backend): release 14.0.10 on
  2026-03-07, commits through 2026-06-29, no deprecation.
- [go2tv](https://github.com/alexballas/go2tv) shipped its **own pure-Go
  Cast stack** in v2.0.0 (2026-01-29) — 17 months *after* hardware
  discontinuation — using only protobuf + hashicorp/mdns.
- [vishen/go-chromecast](https://github.com/vishen/go-chromecast): working
  pure-Go sender, but low-touch (last human commit 2025-04-21, Dependabot
  since). Usable; may need forking.
- The default media receiver app (`CC1AD845`) plays **arbitrary HTTP media
  URLs** — exactly the URL-pull model Heya's stream endpoints already
  serve. The 2025-03 outage was an expired receiver-side cert (fixed by
  Google); 2026-05 "support ending" news was gen-1–3 dongle firmware EOL
  only. Platform risk exists (Google's discretion) but no lockdown observed.
- **No app registration required for senders.** The Cast Developer Console
  ($5, publish flow) is only for *custom receiver apps* (your own HTML5 UI
  on the device — what go2tv registered for) or styled receivers (CSS
  skin). Launching the built-in default receiver needs no registration,
  for audio or video alike. Trade-offs of the default receiver: stock
  Google UI on screen (irrelevant for audio-only speakers), media limited
  to the device's native decoder set (FLAC/MP3/AAC/Opus for audio;
  H.264/VP8/HLS for video), CORS headers required for subtitle tracks.

## 4. DLNA / UPnP AVTransport — clusterfuck confirmed, but shippable

- go2tv (MIT, v2.4.0 2026-07-06) is a shipping Go AVTransport control
  point: `SetAVTransportURI`, `SetNextAVTransportURI`, Play/Pause/Stop,
  GetPositionInfo — with gapless as an **opt-in toggle** "since not all
  devices support gapless playback". Gapless reality is device-dependent.
- [huin/goupnp](https://github.com/huin/goupnp) (BSD-2) ships generated
  MediaRenderer v1 bindings (AVTransport1 incl. SetNextAVTransportURI,
  RenderingControl) but is lightly maintained (last release v1.3.0
  Aug 2023, last commit Apr 2025).
- Cautionary tale: **Jellyfin demoted DLNA out of core** in 10.9 to a
  first-party plugin. The lesson is mostly about renderer-quirk maintenance
  burden, not impossibility.

**Verdict:** reasonable third protocol for legacy reach. Control-point-only
(Play To) is much smaller than also being a DLNA *media server* — Heya
should not implement ContentDirectory browsing, just push stream URLs.

## 5. Vendor local HTTP APIs

- **WiiM — verified.** Official
  [HTTP API PDF v1.2](https://www.wiimhome.com/pdf/HTTP%20API%20for%20WiiM%20Products.pdf):
  `setPlayerCmd:play:url` "plays the URL… points to an audio stream
  address", plus playlist variant. Caveats: served over HTTPS with a
  self-signed device cert; response is always "OK" regardless of outcome.
- **Extracted but NOT verified** (from vendor PDFs; treat as leads):
  - BluOS: unauthenticated local HTTP GET on port 11000, XML responses
    (Custom Integration API v1.7, 2025).
  - Denon HEOS: telnet/TCP :1255, ASCII commands + JSON responses, SSDP
    discovery; HA integration has played arbitrary local HTTP URLs.
  - Yamaha YXC (MusicCast): unauthenticated local HTTP/JSON; HA `play_media`
    pushes arbitrary local stream URLs.

## 6. Closed ecosystems

Roon RAAT, Spotify Connect, Tidal Connect: no third-party *sender* path;
these did not survive verification but nothing suggests otherwise. A Roon
Ready receiver can't be sent Heya audio via RAAT — RAOP/DLNA/vendor API is
the way onto that hardware.

## Sidebar: OwnTone as a wholesale output engine

Instead of (or alongside) building providers, Heya could run
[OwnTone](https://owntone.github.io/owntone-server/) as a sidecar: one GPL C
daemon that already outputs to **AirPlay 1+2 (synced multiroom), Chromecast,
and local ALSA/PulseAudio**, controlled over a documented JSON API
(`/api/player`, `/api/outputs` with per-output volume, `/api/queue`).
Officially supports Linux, FreeBSD, and macOS (dev story OK).

Honest seams (checked 2026-07-09):

- **Audio ingress is not URL-push.** The JSON API's `uris` parameter takes
  *library* identifiers only; arbitrary HTTP stream URLs are not a
  documented queue input — internet radio enters via `.m3u` playlist files
  inside OwnTone's own library. So feeding it per-track Heya stream URLs
  means materializing playlist files into a watched dir (clunky, weak
  metadata) — or better:
- **Pipe mode** (the librespot/shairport integration pattern): OwnTone
  plays from a named FIFO (PCM) with a companion `.metadata` pipe. Heya
  decodes (ffmpeg, same as the RAOP path) and writes PCM + metadata to the
  pipes; OwnTone handles output fan-out and multiroom sync. This neatly
  sidesteps the queue-ownership conflict: **Heya keeps the queue**, OwnTone
  is a dumb synced-output engine.
- **Second daemon**: own config, own SQLite, avahi/mDNS deps in the
  container, another process to supervise. Acceptable as an *optional*
  feature (prod is a container; Postgres is already an external dep), but
  against the single-binary grain for the default install.
- **Skip its server half.** DAAP/iTunes, Roku RSP, and its library scanner
  would create a second index over the same files — point it at an empty
  library and use pipes only. The value is exclusively the output side.
- Signal worth noting: Music Assistant chose to *extract* slim binaries
  (cliraop/cliap2) from the OwnTone lineage rather than run OwnTone whole —
  queue ownership and deployment weight are presumably why. Using OwnTone
  directly does get the more mature AP2 stack (PTP timing, password
  pairing) than cliap2 has today.

Verdict: strong prototype path, and the cheapest route to **AirPlay 2 +
synced multiroom** specifically. Long-term it should be one provider
("OwnTone output engine", pipe mode) behind the same cast-provider
abstraction — not the abstraction itself.

## Architecture sketch for Heya

Music Assistant's shape, adapted:

1. **`internal/castout/` (or similar) provider abstraction** — each
   protocol implements: `Discover()` (mDNS/SSDP, continuous), `Connect()`,
   `Play(streamURL | pcmPipe)`, `Pause/Stop/Seek/SetVolume`, state events.
   Renderers surface in the existing WS event bus so all clients see the
   same cast state live.
2. **Two transport shapes:**
   - *URL-pull* (Cast, DLNA, WiiM/vendor): hand the receiver
     `/api/music/tracks/{id}/stream?token=…` — range-serving, AAC
     transcode fallback, and `?token=` query auth already exist today.
     Needs a scoped/short-lived cast token rather than the user session
     token.
   - *PCM-push* (RAOP): ffmpeg decode → stdin of cliraop subprocess.
     Mirrors the existing transcoder session management.
3. **Server-side playback session** — queue position, elapsed time, volume
   live in the server (it's the sender), so any client can pick up/control
   the session. This is the same model as the sonic/transcode session
   registries.
4. **Single-binary constraint:** cliraop is a C binary. Options: embed
   per-platform binaries and extract at runtime (license diligence first),
   add to container images alongside jellyfin-ffmpeg (prod is a container
   anyway), or eventually port RAOP to pure Go. Cast/DLNA/vendor paths are
   pure Go from day one.

### Suggested order

1. **Google Cast v2** — smallest end-to-end proof of the whole cast UX
   (discovery → picker → play/pause/volume/queue) in pure Go.
2. **RAOP/AirPlay 1** — the protocol that actually reaches the hi-fi
   receiver class; cliraop subprocess first.
3. **WiiM/vendor API** — if the house receiver matches, ~a day.
4. **DLNA AVTransport** — legacy breadth, quirks budget required.
5. **AirPlay 2 (cliap2)** — only for RAOP-broken devices, if ever.

## Open questions

- Which receiver(s) do we actually target first? (Vendor API choice hinges
  on brand — WiiM/BluOS/HEOS/MusicCast all differ.)
- libraop redistribution terms (no LICENSE file in repo). Note: cliairplay
  (cliap2) is clean GPL-2.0, so the AP2 binary has no such problem — and
  could even substitute for cliraop entirely if AP2-mode proves reliable
  on the target hardware.
- ~~Has MA's cliap2 PTP PR merged?~~ Resolved: PTP timing merged
  2026-03-16 (cliairplay #98). Password pairing still missing.
- Real-world RAOP latency + DLNA gapless behavior on the specific target
  hardware — needs empirical testing once a provider exists.
