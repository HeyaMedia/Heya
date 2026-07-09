# cliap2 binaries

Prebuilt `cliap2` AirPlay 2 sender binaries, embedded into the Heya
binary via `go:embed` and extracted to `<DataDir>/cast/bin/` at runtime
(see `airplay_binary.go`).

- **Upstream**: https://github.com/music-assistant/cliairplay
  ("Based on OwnTone", https://github.com/owntone/owntone-server)
- **License**: GPL-2.0 (upstream `LICENSE`/`COPYING`). cliap2 runs as a
  separate subprocess — Heya invokes it over stdin/stderr/FIFO and does
  not link it. Distributing these binaries requires making the
  corresponding source available: it is the upstream repository at the
  pinned commit below.
- **Pinned commit**: `3bb9271643999696638ee5df421b69bb5112fb32`
  (main, 2026-05-03, "Merge pull request #111 from music-assistant/dev")
- **Built by**: upstream GitHub Actions run 25268546639 (artifacts
  `cliap2-{macos,linux}-{arm64/aarch64,x86_64}`), fetched 2026-07-10.
- **ABI note**: Linux builds target Debian Bookworm shared libraries
  (libplist, libconfuse, libevent, libsodium, json-c, libgcrypt, system
  libav*) — the container images install these. macOS builds link
  Homebrew dylibs (ffmpeg, libplist, confuse, zlib, libiconv, …) — dev
  boxes need `brew install ffmpeg libplist confuse zlib libiconv`.

## Updating

1. Pick an upstream commit; note it here and re-validate the stderr
   contract in `airplay_stderr.go` (`device_activate_cb (status 2)`,
   `event_play_start`, `end of stream reached`, `Pause at`,
   `closed RTSP connection`) — we deliberately carry no upstream
   patches, but we also intentionally have NOT reported the v1.5
   commence-wedge bug (see docs/casting-research.md), so behavior may
   shift under us on a bump.
2. `gh run list -R music-assistant/cliairplay` → download the four
   artifacts from a green run on that commit.
3. Replace the binaries, update the pinned commit + run ID above.
4. `go test ./internal/cast/` and a live `heya cast play` smoke test.
