# CLI reference

The `heya` binary is the only thing you need to operate the server. Every
feature is reachable through it — there's no admin endpoint or web setting
that isn't also a CLI command.

`heya --help` for the live tree. `--json` (machine output) and `--no-color`
are global flags.

## Server lifecycle

| Command           | What it does                                                |
| ----------------- | ----------------------------------------------------------- |
| `heya serve`      | Start the HTTP server (default `:8080`, `HEYA_PORT` to override) |
| `heya dev-proxy`  | Dev front-door reverse proxy on `:8080` (normally launched by `make dev`/mprocs, not run directly) |
| `heya dashboard`  | Full-screen TUI: server status, queue, scans, watchers      |
| `heya setup`      | Guided first-time config (writes `.env` and seeds admin)    |

For local development use `make dev` (or `make dev-front` + `make dev-go`
+ `make dev-web` in three terminals). It uses **mprocs** to start
`heya dev-proxy` on `:8080` (the front door), the backend
(`heya serve --dev-backend`) on `:3050` under air, and Nuxt on `:3000`.
Install with `brew install mprocs`. See [development.md](development.md) for
the rationale.

## Libraries

| Command                       | What it does                                  |
| ----------------------------- | --------------------------------------------- |
| `heya library list`           | List libraries with type, path, watcher state |
| `heya library add`            | Add a library (interactive or `--path` flags) |
| `heya library remove <id>`    | Remove a library                              |
| `heya library info <id>`      | Show library details + settings               |
| `heya library settings <id>`  | Update per-library settings                   |
| `heya library scan <id>`      | Trigger a scan                                |
| `heya library files <id>`     | List files known to a library                 |
| `heya library stats <id>`     | Match/unmatched/error counts                  |
| `heya library watch`          | Show fsnotify watcher status                  |

## Media items

| Command                          | What it does                                |
| -------------------------------- | ------------------------------------------- |
| `heya media list`                | List media items (`--type movie\|tv\|…`)    |
| `heya media info <id\|slug>`     | Full detail dump for one item               |
| `heya media search <query>`      | Full-text search                            |
| `heya media match`               | Re-run matching for unmatched files         |
| `heya media refresh <id\|slug>`  | Re-fetch metadata from `metadata-server`    |

## Queue & jobs

| Command                | What it does                                    |
| ---------------------- | ----------------------------------------------- |
| `heya queue status`    | Counts by state (pending / running / failed)    |
| `heya queue process`   | Process queued jobs until empty (CLI worker)    |
| `heya queue clear`     | Remove completed + failed jobs                  |
| `heya job list`        | Detailed job list with payload + retry info     |
| `heya job status <id>` | One job's full state                            |

## Music analysis (sonic pipeline)

| Command                       | What it does                                                   |
| ----------------------------- | -------------------------------------------------------------- |
| `heya analyze status`         | Show analyzer + fetcher state + pending count                  |
| `heya analyze run`            | Run one analyzer pass now (ignores schedule window)            |
| `heya analyze reset`          | Force re-analysis of all (or one library's) tracks             |
| `heya analyze fetch-models`   | Download missing model files (blocking)                        |
| `heya analyze warmup`         | Load every model + run a smoke-test inference                  |

## Transcoding

| Command                          | What it does                                 |
| -------------------------------- | -------------------------------------------- |
| `heya transcode probe <file>`    | ffprobe a file through Heya's analyzer       |
| `heya transcode test <file>`     | Dry-run the decision matrix against a client |
| `heya transcode cache`           | Inspect / clear the HLS segment cache        |

## Database & migrations

| Command                | What it does                              |
| ---------------------- | ----------------------------------------- |
| `heya migrate up`      | Apply pending application + River migrations |
| `heya migrate down`    | Roll back one migration                   |
| `heya migrate status`  | Show applied vs pending                   |
| `heya migrate reset`   | Roll back all migrations                  |
| `heya db:wipe`         | Drop media tables (preserves users)       |

## Users

| Command                                              | What it does                     |
| ---------------------------------------------------- | -------------------------------- |
| `heya user create --username … --email … [--admin]`  | Create a user                    |
| `heya user list`                                     | List users                       |
| `heya user delete <username>`                        | Delete a user                    |
| `heya user reset-password <username>`                | Reset password (prompts for new) |

## Config

Config is env-only (`.env` → `.env.local` → process env, defaults applied
last). There is no `heya.yaml`.

| Command                      | What it does                                       |
| ---------------------------- | -------------------------------------------------- |
| `heya config show`           | Print current config with `Source ∈ {env,db,default}` per field |

`/api/config/sources` returns the same provenance map; the Settings UI uses
it to grey out env-locked fields.

## Tailscale

| Command                          | What it does                                           |
| -------------------------------- | ------------------------------------------------------ |
| `heya tailscale status [--json]` | Show current node state                                |
| `heya tailscale logout`          | Wipe local identity (re-onboard on next start)         |
| `heya remote status [--json]`    | Remote-access state machine (phase, port, IPs, cert)   |
| `heya remote check`              | Re-assert the port mapping + re-run the outside-in check |
| `heya remote enable` / `disable` | Toggle remote access (disable unmaps the router port)  |

Run while `heya serve` is **not** running — both would race for the
state dir. See [tailscale.md](./tailscale.md) for the full integration.

## Local API client

`heya api <method> <path> [body]` issues an authenticated request to the
running server. First call logs in, caches the bearer token under the OS user
config dir, and reuses it. Full details in
[development.md](./development.md#hitting-the-local-api).

```bash
heya api get /api/health
heya api get /api/music/artists -q limit=5
heya api post /api/users '{"username":"bob","email":"b@x","password":"hunter22"}'
cat patch.json | heya api patch /api/media/42 -
```

| Flag       | Purpose                                                |
| ---------- | ------------------------------------------------------ |
| `--base`   | Server base URL (default `http://localhost:8080`)      |
| `--user`   | Login username (default `admin`)                       |
| `--pass`   | Login password (default `admin`)                       |
| `--token`  | Bearer token (skips login + cache)                     |
| `-q k=v`   | Query param (repeatable, URL-encoded)                  |
| `--raw`    | Stream response bytes verbatim (no JSON pretty-print)  |

## OpenAPI

| Command                          | What it does                                          |
| -------------------------------- | ----------------------------------------------------- |
| `heya openapi-spec [-o file]`    | Dump the generated OpenAPI 3.1 document               |

Used by `make gen-api-client` to regenerate `web/shared/api.openapi.json`.

## Misc

| Command                  | What it does                                              |
| ------------------------ | --------------------------------------------------------- |
| `heya parse <name>`      | Run the filename parser against a path (debug aid)        |
| `heya studios sync`      | Download production-company logos                         |

## Examples

```bash
# Bootstrap a fresh install
make db-up && make build
./bin/heya setup
./bin/heya user create --username admin --email me@example.com --admin

# Add a library and scan it
./bin/heya library add --name "Films" --type movie --path /mnt/films
./bin/heya library scan 1

# Debug a missing match
./bin/heya parse "/mnt/films/Dune Part Two (2024).mkv"
./bin/heya media search "Dune"
./bin/heya media refresh dune-part-two-2024

# Check why a file is transcoding
./bin/heya transcode probe /mnt/films/Dune.Part.Two.2024.mkv
./bin/heya transcode test /mnt/films/Dune.Part.Two.2024.mkv --client safari

# Inspect sonic-analysis state and reset it
./bin/heya analyze status
./bin/heya analyze reset --library 3

# Live ops
./bin/heya dashboard
./bin/heya queue status
./bin/heya job list --state failed
```
