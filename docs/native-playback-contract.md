# Native desktop playback contract

Status: protocol v1 implemented for browser and HeyaClient MPV playback. This
document intentionally does not grant the remote web UI general Tauri IPC
access.

## Ownership

- Heya owns authentication, queue/up-next policy, progress reporting, source
  selection, and the visible controls.
- HeyaClient owns native renderer lifecycle, MPV integration, native windows,
  property observation, and validation at the bridge boundary.
- The active playback backend owns the authoritative clock and renderer state.
- Heya's server owns source/transcode facts. Renderer diagnostics supplement
  those facts; they never replace them.
- The `heya_client=1` marker and client-surface header are untrusted metadata.
  They MUST NOT enable the native bridge. Heya selects the native backend only
  after a successful versioned capability handshake supplied by HeyaClient.

The remote page MUST NOT receive generic Tauri `invoke`, raw MPV commands,
property setters, arbitrary property subscriptions, arbitrary HTTP headers,
filesystem paths, shell commands, or MPV arguments.

## Names and identifiers

Do not overload the word "session":

- `rendererSessionId`: HeyaClient-generated identity for one native load. Used
  to reject late commands/events after a new item is loaded.
- `commandId`: Heya-generated identity for command acknowledgement and retry
  deduplication.
- `playbackGrant`: opaque, server-issued, narrowly scoped media credential.
  This is not the user's Heya API bearer token.
- Heya's progress/now-playing session remains an independent server concept.

State and diagnostics use independent, monotonically increasing revisions per
renderer session because they are published at different rates.

```ts
interface NativeStateEvent<T> {
  protocolVersion: 1
  rendererSessionId: string
  stateRevision: number
  payload: T
}

interface NativeDiagnosticsEvent<T> {
  protocolVersion: 1
  rendererSessionId: string
  diagnosticsRevision: number
  payload: T | null
}
```

Heya MUST discard an event when its protocol version is unsupported, its
renderer session is no longer active, or its revision is not newer than the
last accepted revision for that channel.

## Semantic bridge

Exact transport naming is owned by HeyaClient, but the exposed surface is
limited to these semantic operations:

```ts
getPlaybackCapabilities(): Promise<PlaybackCapabilities>
loadPlayback(request): Promise<{ rendererSessionId: string }>
sendPlaybackCommand(command): Promise<CommandResult>
subscribePlaybackState(listener): Unsubscribe
subscribePlaybackDiagnostics(listener): Unsubscribe
disposePlayback({ rendererSessionId }): Promise<void>
```

Commands contain `rendererSessionId` and `commandId` and use desired state,
not toggles:

- `play`
- `pause`
- `seek` with an absolute position in seconds
- `setVolume` with a normalized value in the inclusive range 0..1
- `setMuted`
- `setFullscreen`
- `selectAudioTrack` with a normalized stable track ID
- `selectSubtitleTrack` with a normalized stable track ID or `null`
- `selectVariant` with a normalized server variant ID
- `stop`

HeyaClient serializes commands per renderer session. Acknowledgement means the
command was accepted or rejected; playback state events remain authoritative.
Track IDs exposed to Heya are normalized IDs. Raw MPV `aid`, `sid`, `vid`, and
HLS variant numbers remain private implementation details.

Quality selection is a server/source operation. It may require a replacement
manifest or playback grant rather than an MPV property change.

## Authoritative state and termination

State is separate from diagnostics and includes the current position,
duration, play/pause/buffering/loading flags, volume/mute/fullscreen state,
available/selected normalized tracks, and typed errors.

Termination reasons are distinct:

- `ended`: natural end-of-file; Heya may complete progress and run up-next.
- `stopped`: explicit user stop.
- `window_closed`: user closed the native player window.
- `disposed`: the owning Heya playback session disappeared.
- `failed`: media or renderer failure.
- `native_crashed`: HeyaClient lost the renderer unexpectedly.
- `logged_out`: Heya authentication ended.
- `server_switched`: HeyaClient changed selected server.
- `app_quit`: HeyaClient is exiting.

Only `ended` is completion. Closing, stopping, disposal, or failure MUST NOT be
translated into an ended event.

Initial lifecycle policy: native playback stops when the owning Heya page is
unloaded, logs out, switches server, closes the main/native player window, or
quits. Surviving navigation/WebView suspension is a later explicit feature.

## Diagnostics

Diagnostics are optional and non-authoritative. They can be absent or become
unavailable without affecting playback. Heya's normalized shape lives in
`web/app/types/video-playback.ts`.

Recommended MPV sources:

| Heya field | MPV property |
| --- | --- |
| source codec/profile/tracks | `track-list` / `current-tracks` |
| decoded/output video parameters | `video-params`, `video-out-params` |
| pixel/color/HDR metadata | `video-params/*` and selected track metadata |
| measured decoder/filter FPS | `estimated-vf-fps` |
| packet bitrate | `video-bitrate`, `audio-bitrate` |
| hardware decoder/interop | `hwdec-current`, `hwdec-interop` |
| decoded audio format | `audio-params` |
| audio API output format | `audio-out-params` |
| renderer/decoder drops | `frame-drop-count`, `decoder-frame-drop-count` |
| mistimed output frames | `mistimed-frame-count` |
| A/V drift | `avsync` (seconds; normalize to milliseconds) |
| buffer/network health | `demuxer-cache-state`, `cache-speed` |

Do not query or expose `perf-info`. Do not forward MPV property-change events
directly. HeyaClient observes an allowlist, strips paths, filenames, URLs,
headers, grants, and other secrets, and publishes coalesced normalized
snapshots:

- structural state and errors immediately;
- position around 4 Hz;
- diagnostics around 1 Hz;
- track, format, and capability changes on reconfiguration.

MPV measurements are often approximate. In particular, source/output audio
format equality does not prove that an OS mixer or driver preserved the signal
bit-for-bit. `audio-out-params` describes data written to the audio API, not
necessarily the final DAC format.

## Playback grants

The user's Heya API bearer token MUST NOT be passed to HeyaClient or MPV.
Native streaming requires a separate opaque playback grant scoped to one user,
server, media item, and short-lived playback session. It must cover the whole
stream graph where applicable:

- master and variant manifests;
- media segments and byte-range requests;
- encryption keys;
- subtitle streams;
- seeking and quality replacement;
- renewal for long playback.

The remote UI cannot supply arbitrary HTTP headers. It requests a grant with
`POST /api/playback/native/grants`; the response contains a relative media
path and opaque grant. HeyaClient accepts only HTTP(S) URLs belonging to the
selected Heya origin below `/api/playback/native/media/` and constructs:

`X-Heya-Playback-Grant: <opaque grant>`

The grant is stored hashed in Heya, bound to the login session that issued it,
scoped to one media subtree, and capped at 12 hours. Direct byte ranges, HLS
manifests/segments, and subtitles revalidate both grant and login session on
every request. These routes do not redirect. Local files and arbitrary MPV
protocols are rejected.

The v1 grant registry is process-local. A future multi-replica Heya deployment
must move it to shared storage (or a revocable shared signing design) before
native playback can be load-balanced across replicas.

## Backend selection and fallback

- Browser/PWA always use the existing HTML media/HLS backend.
- HeyaClient may prefer MPV only after the native capability handshake.
- Browser playback remains a user-selectable fallback/output preference.
- Automatic fallback is safe before native media starts, or after HeyaClient
  confirms the renderer has stopped and been disposed.
- Heya MUST NOT start browser playback while native playback may still be
  active. Mid-play failure initially requires an explicit retry/fallback.

## Delivery phases

1. Wrap the current browser player in the backend-neutral adapter and separate
   state from diagnostics without changing browser behavior.
2. Add a fake native adapter and race/lifecycle contract checks in Heya.
3. Spike libmpv on Apple Silicon, Windows, Linux X11/Wayland, multiple
   monitors, fullscreen, resize, teardown, hardware decode, packaging, signing,
   and licensing. Use a separate native player window first.
4. Add the narrow HeyaClient bridge and scoped server playback grants. (Done)
5. Add MPV diagnostics and normalized track selection. (Done; variants reload
   a server HLS descriptor rather than setting an MPV property.)
6. Evaluate an integrated render surface independently from transport/control.
7. Prove native audio through MPV before adding a separate Rust bit-perfect
   engine behind the same semantic backend contract.

## Primary references

- MPV command/property events: <https://github.com/mpv-player/mpv/blob/master/DOCS/man/input.rst>
- MPV property reference: <https://mpv.io/manual/master/>
- Official libmpv examples and render API guidance:
  <https://github.com/mpv-player/mpv-examples/tree/master/libmpv>
- MPV licensing: <https://github.com/mpv-player/mpv#license>
