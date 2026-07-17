# Heya

Self-hosted media server for films, TV, music, and books — one Go binary with
separate serving and background-worker roles, the Nuxt SPA embedded, and
Postgres as the only datastore. Canonical metadata
comes from HeyaMetadata V2 (TMDB, TVDB, AniDB, MusicBrainz, and OpenLibrary
reconciled behind one UUID-based contract), so Heya stays focused on libraries,
playback, and the UI. Community skip segments are fetched directly by Heya.

- **Libraries** — local + SMB mounts, filesystem watch + periodic re-scan, NFO-aware matching
- **Playback** — HLS transcoding with per-client capability negotiation, ASS subtitles, trickplay
- **Music** — web player with gapless/crossfade, sonic analysis on the server
- **Users** — per-user watch state, favorites and lists, session auth
- **Jellyfin-compatible API** — point Infuse, Finamp, Streamyfin, Findroid & co. at Heya
- **Tailscale** — optional [tsnet](https://tailscale.com/docs/features/tsnet) node: reach your server over your tailnet, no port forwarding
- **CLI-first** — everything the server does is a `heya` subcommand, plus a dashboard TUI

## Quick start (Docker)

```bash
mkdir -p "$HOME/heya-data"
docker run -d --name heya -p 8080:8080/tcp -p 8080:8080/udp \
  -v "$HOME/heya-data:/data" \
  -v "/path/to/your/media:/media:ro" \
  -e HEYA_METADATA_URL='https://your-heyametadata.example' \
  -e HEYA_ADMIN_USERNAME=admin \
  -e HEYA_ADMIN_PASSWORD=admin \
  ghcr.io/heyamedia/heya:latest-aio
```

Replace `/path/to/your/media` with the absolute path to your films, shows,
music, or books, and set `HEYA_METADATA_URL` to a reachable HeyaMetadata V2
deployment, then add `/media` as a library in Heya. `$HOME/heya-data`
holds PostgreSQL and all other Heya state, making it straightforward to inspect
and back up. A Docker named volume also works, but a normal host path is
recommended. Or use the regular image against your own Postgres:

```bash
docker run -p 8080:8080/tcp -p 8080:8080/udp -v $PWD/data:/data \
  -e HEYA_DATABASE_URL='postgres://heya:heya@db:5432/heya?sslmode=disable' \
  -e HEYA_METADATA_URL='https://your-heyametadata.example' \
  ghcr.io/heyamedia/heya:latest
```

The AIO image supervises Postgres, `heya serve`, and `heya worker` inside the
single container. Regular-image deployments must run both Heya commands from
the same image; the repository's Docker Compose file is the simplest example.

Open https://localhost:8080 and sign in with `admin` / `admin`, then change the
password and add your libraries under Settings → Libraries. Heya creates a
private Caddy CA on first boot; install `data/caddy/pki/authorities/local/root.crt`
in clients you want to trust, or accept the initial browser warning. UDP is
published beside TCP so HTTP/3 works. Hardware transcode
and the CUDA/OpenVINO image variants are covered in
[docs/deployment.md](docs/deployment.md). Every operational knob is an env var
— [`.env.example`](.env.example) documents them all.

## From source

Needs Go 1.26+, [Bun](https://bun.sh), Docker, and `ffmpeg`/`ffprobe` on
`$PATH`:

```bash
cp .env.example .env
make db-up                  # Postgres on :5440
make build                  # Nuxt (bun) + Go → ./bin/heya
./bin/heya setup            # guided wizard: migrations, admin user, first library
./bin/heya worker           # terminal 1: queue, scheduler, filesystem watchers
./bin/heya serve            # terminal 2: HTTPS API/SPA (H1 + H2 + H3)
```

Day-to-day development (hot reload via `make dev`, tests, hooks, CI) lives in
[docs/development.md](docs/development.md).

## Docs

| Doc                                          | What's inside                                    |
| -------------------------------------------- | ------------------------------------------------ |
| [architecture.md](docs/architecture.md)      | Repo layout, request lifecycle, design choices   |
| [development.md](docs/development.md)        | Dev workflow, builds, DB, tests, hooks, CI       |
| [deployment.md](docs/deployment.md)          | Container images, GPU variants, Docker Compose   |
| [pipeline.md](docs/pipeline.md)              | Scan → match → enrich pipeline                   |
| [cli.md](docs/cli.md)                        | Full CLI reference                               |
| [jellyfin.md](docs/jellyfin.md)              | Jellyfin-compatible API                          |
| [tailscale.md](docs/tailscale.md)            | tsnet integration                                |
| [system-media.md](docs/system-media.md)      | Browser/PWA and native OS media integration      |
| [ui.md](docs/ui.md)                          | Frontend conventions                             |

The HTTP API documents itself at `/api/docs` (Scalar over OpenAPI 3.1 via
Huma v2); real-time events stream over the `/api/ws` WebSocket.

## Contributing

[`CLAUDE.md`](CLAUDE.md) is the contributor entry point — toolchain rules
(bun only, never npm), hard conventions, and pointers into `docs/`.

## License

MIT — see [`LICENSE`](LICENSE).
