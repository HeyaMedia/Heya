#!/usr/bin/env bash

set -euo pipefail

# Keep Air out of the user's global Go cache. Constant source changes generate
# new cache entries faster than Go's age-based cache trimming can remove them.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOCACHE="${HEYA_AIR_GOCACHE:-$repo_root/.cache/go-build-air}"
export GOMODCACHE="${HEYA_GOMODCACHE:-$repo_root/.cache/go-mod}"

mkdir -p "$repo_root/tmp" "$GOCACHE" "$GOMODCACHE"
air_output="${HEYA_AIR_OUTPUT:-tmp/heya}"

# All three dev panes (api air, worker air, proxy) build the SAME package —
# without coordination a single Go save fires two full compile fan-outs at
# once (three at mprocs startup), each racing the other for the build cache.
# Serialize through a lock and build ONE shared intermediate: the first
# caller does the real compile+link, every later caller's `go build` is a
# no-op (Go compares build IDs against the -o target and skips), and each
# pane just copies the result to its own name.
#
# mkdir is the portable atomic lock on macOS (no flock(1)). A PID file lets
# us steal locks left behind by a SIGKILLed build instead of deadlocking.
lock_dir="$repo_root/tmp/.heya-build-lock"
while ! mkdir "$lock_dir" 2>/dev/null; do
  holder="$(cat "$lock_dir/pid" 2>/dev/null || true)"
  if [ -n "$holder" ] && ! kill -0 "$holder" 2>/dev/null; then
    rm -rf "$lock_dir"
    continue
  fi
  sleep 0.2
done
echo $$ > "$lock_dir/pid"
trap 'rm -rf "$lock_dir"' EXIT

shared_bin="$repo_root/tmp/.heya-build"
go build -o "$shared_bin" "$repo_root/cmd/heya"

# Copy-then-rename: the pane's old binary may still be EXECUTING while we
# replace it (air stops it after the build). rename() swaps the directory
# entry and leaves the running process its old inode; writing into the live
# file instead risks a macOS code-signature kill of the running process.
cp "$shared_bin" "$repo_root/$air_output.next.$$"
mv -f "$repo_root/$air_output.next.$$" "$repo_root/$air_output"

# Cache entries are disposable. Bound Air's cache instead of letting a long
# dev session consume tens of gigabytes. Override in KiB if ever needed.
max_cache_kib="${HEYA_AIR_CACHE_MAX_KIB:-8388608}"
cache_kib="$(du -sk "$GOCACHE" | awk '{print $1}')"
if (( cache_kib > max_cache_kib )); then
  echo "Air Go cache reached $((cache_kib / 1024)) MiB; clearing compiled artifacts (limit: $((max_cache_kib / 1024)) MiB)"
  go clean -cache
fi
