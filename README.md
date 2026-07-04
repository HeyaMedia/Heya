# Heya

Self-hosted media server for films, TV, music, and books — a single Go binary
with the Nuxt SPA embedded, Postgres as the only datastore. Metadata comes
from [HeyaMedia/metadata-server](https://github.com/HeyaMedia/metadata-server)
(TMDB, TVDB, AniDB, MusicBrainz, OpenLibrary behind one aggregator), so Heya
stays focused on libraries, playback, and the UI.

- **Libraries** — local + SMB mounts, filesystem watch + periodic re-scan, NFO-aware matching
- **Playback** — HLS transcoding with per-client capability negotiation, ASS subtitles, trickplay
- **Music** — web player with gapless/crossfade, sonic analysis on the server
- **Users** — per-user watch state, favorites and lists, session auth
- **Jellyfin-compatible API** — point Infuse, Finamp, Streamyfin, Findroid & co. at Heya
- **Tailscale** — optional [tsnet](https://tailscale.com/docs/features/tsnet) node: reach your server over your tailnet, no port forwarding
- **CLI-first** — everything the server does is a `heya` subcommand, plus a dashboard TUI

## Quick start (Docker)

```bash
docker compose up -d        # Postgres + ghcr.io/heyamedia/heya:latest on :8080
```

Or standalone against your own Postgres:

```bash
docker run -p 8080:8080 -v $PWD/data:/data \
  -e HEYA_DATABASE_URL='postgres://heya:heya@db:5432/heya?sslmode=disable' \
  ghcr.io/heyamedia/heya:latest
```

Open http://localhost:8080, create the admin user, add your libraries under
Settings → Libraries. Hardware transcode and the CUDA/OpenVINO image variants
are covered in [docs/deployment.md](docs/deployment.md). Every operational
knob is an env var — [`.env.example`](.env.example) documents them all.

## From source

Needs Go 1.26+, [Bun](https://bun.sh), Docker, and `ffmpeg`/`ffprobe` on
`$PATH`:

```bash
cp .env.example .env
make db-up                  # Postgres on :5440
make build                  # Nuxt (bun) + Go → ./bin/heya
./bin/heya setup            # guided wizard: migrations, admin user, first library
./bin/heya serve            # http://localhost:8080
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
| [ui.md](docs/ui.md)                          | Frontend conventions                             |

The HTTP API documents itself at `/api/docs` (Scalar over OpenAPI 3.1 via
Huma v2); real-time events stream over the `/api/ws` WebSocket.

## Contributing

[`CLAUDE.md`](CLAUDE.md) is the contributor entry point — toolchain rules
(bun only, never npm), hard conventions, and pointers into `docs/`.

## License

MIT — see [`LICENSE`](LICENSE).
