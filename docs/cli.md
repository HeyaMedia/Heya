# CLI reference

The `heya` binary is the only thing you need to operate the server. Every
feature is reachable through it — there's no admin endpoint or web setting
that isn't also a CLI command.

`heya --help` for the live tree. `--json` (machine output) and `--no-color`
are global flags.

## Server lifecycle

| Command            | What it does                                                |
| ------------------ | ----------------------------------------------------------- |
| `heya serve`       | Start the HTTP server (default `:8080`)                     |
| `heya dev`         | Spawn Go API (air hot-reload) + Nuxt dev server in parallel |
| `heya dashboard`   | Full-screen TUI: server status, queue, scans, watchers      |
| `heya setup`       | Guided first-time config (writes `heya.yaml`)               |

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

## Transcoding

| Command                          | What it does                                 |
| -------------------------------- | -------------------------------------------- |
| `heya transcode probe <file>`    | ffprobe a file through Heya's analyzer       |
| `heya transcode test <file>`     | Dry-run the decision matrix against a client |
| `heya transcode cache`           | Inspect / clear the HLS segment cache        |

## Database & migrations

| Command                | What it does                              |
| ---------------------- | ----------------------------------------- |
| `heya migrate up`      | Apply pending migrations (goose)          |
| `heya migrate down`    | Roll back one migration                   |
| `heya migrate status`  | Show applied vs pending                   |
| `heya migrate reset`   | Roll back all migrations                  |
| `heya db:wipe`         | Drop media tables (preserves users)       |

## Users

| Command                                | What it does                                |
| -------------------------------------- | ------------------------------------------- |
| `heya user create --username … --email … [--admin]` | Create a user           |
| `heya user list`                       | List users                                  |
| `heya user delete <username>`          | Delete a user                               |
| `heya user reset-password <username>`  | Reset password (prompts for new)            |

## Config

| Command                      | What it does                                |
| ---------------------------- | ------------------------------------------- |
| `heya config show`           | Print current config + value sources        |
| `heya config path`           | Show which `heya.yaml` is loaded            |
| `heya config init`           | Write a default `heya.yaml`                 |
| `heya config set <key> <v>`  | Update a key in `heya.yaml`                 |

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

# Live ops
./bin/heya dashboard
./bin/heya queue status
./bin/heya job list --state failed
```
