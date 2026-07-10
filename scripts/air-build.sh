#!/usr/bin/env bash

set -euo pipefail

# Keep Air out of the user's global Go cache. Constant source changes generate
# new cache entries faster than Go's age-based cache trimming can remove them.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOCACHE="${HEYA_AIR_GOCACHE:-$repo_root/.cache/go-build-air}"
export GOMODCACHE="${HEYA_GOMODCACHE:-$repo_root/.cache/go-mod}"

mkdir -p "$repo_root/tmp" "$GOCACHE" "$GOMODCACHE"
go build -o "$repo_root/tmp/heya" "$repo_root/cmd/heya"

# Cache entries are disposable. Bound Air's cache instead of letting a long
# dev session consume tens of gigabytes. Override in KiB if ever needed.
max_cache_kib="${HEYA_AIR_CACHE_MAX_KIB:-8388608}"
cache_kib="$(du -sk "$GOCACHE" | awk '{print $1}')"
if (( cache_kib > max_cache_kib )); then
  echo "Air Go cache reached $((cache_kib / 1024)) MiB; clearing compiled artifacts (limit: $((max_cache_kib / 1024)) MiB)"
  go clean -cache
fi
