#!/usr/bin/env bash
#
# Launch Heya in ACTIVE mode against an ISOLATED local dev DB + the fulldata/
# sample libraries, for testing the local-first ingest pipeline.
#
# This is the ONLY supported way to run the dev-active setup. It sets active
# mode and the local DB TOGETHER, exported (so they outrank both .env and any
# prod HEYA_* already exported in your shell), and then HARD-REFUSES to start if
# the DB host isn't local. A persistent .env.local could not give this
# guarantee: process-env HEYA_DATABASE_URL outranks the file, so active mode
# could silently pair with a prod DB and run workers/scanner against production.
#
# A plain `make dev` (without this script) stays SAFE — it uses .env's
# passive-mode prod-mirror defaults. Active mode is opt-in via this launcher.
#
# Defense in depth: the binary ITSELF now refuses active mode against a
# non-local DB (serve.go guard on HEYA_ALLOW_REMOTE_ACTIVE; --dev-backend can
# never opt in). This launcher just makes the safe local-active path one command.
#
# Usage:  scripts/dev-local.sh         # full mprocs front door (default)
#         scripts/dev-local.sh serve   # ./bin/heya serve directly (needs `make build-go` first)
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# --- dev environment: exported, so it beats .env AND any inherited prod vars ---
export HEYA_PASSIVE_MODE=false
export HEYA_DATABASE_URL="postgres://heya:heya@localhost:5440/heya_dev?sslmode=disable"
export HEYA_DATA_DIR="./data/dev"
export HEYA_IMAGE_PROXY_URL=""
export HEYA_LIBRARY_0_NAME=DevMusic
export HEYA_LIBRARY_0_PATHS="$ROOT/fulldata/Music"
export HEYA_LIBRARY_0_TYPE=music
export HEYA_LIBRARY_1_NAME=DevTV
export HEYA_LIBRARY_1_PATHS="$ROOT/fulldata/TV"
export HEYA_LIBRARY_1_TYPE=tv
export HEYA_LIBRARY_2_NAME=DevMovies
export HEYA_LIBRARY_2_PATHS="$ROOT/fulldata/Movies"
export HEYA_LIBRARY_2_TYPE=movie

# --- HARD GUARD: active mode must never touch a non-local DB host ---
host="$(printf '%s' "$HEYA_DATABASE_URL" | sed -E 's#^[a-z]+://[^@]*@([^/:]+).*#\1#')"
case "$host" in
  localhost|127.0.0.1|::1) ;;
  *)
    echo "ABORT: active mode against non-local DB host '$host'." >&2
    echo "       This launcher only runs against a local dev DB." >&2
    exit 1 ;;
esac

# --- the local Postgres must be up (make db-up) ---
if ! docker exec -i heya-postgres true 2>/dev/null; then
  echo "ABORT: postgres container 'heya-postgres' is not running. Run: make db-up" >&2
  exit 1
fi

# --- ensure the isolated dev DB exists (no-op once created) ---
if ! docker exec -i heya-postgres psql -U heya -lqt 2>/dev/null | cut -d'|' -f1 | grep -qw heya_dev; then
  echo "creating isolated dev database 'heya_dev'..."
  docker exec -i heya-postgres createdb -U heya heya_dev
fi

echo "starting Heya: active mode, DB=heya_dev (localhost), libraries=Dev{Music,TV,Movies} -> fulldata/"
case "${1:-dev}" in
  dev)   exec make dev ;;
  serve)
    ./bin/heya worker &
    worker_pid=$!
    trap 'kill "$worker_pid" 2>/dev/null || true; wait "$worker_pid" 2>/dev/null || true' EXIT INT TERM
    ./bin/heya serve
    ;;
  *)     echo "usage: $0 [dev|serve]" >&2; exit 2 ;;
esac
