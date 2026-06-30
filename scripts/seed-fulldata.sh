#!/usr/bin/env bash
#
# Seed ./fulldata with a small, real subset pulled from the NAS, for testing the
# local-first ingest pipeline end-to-end against genuine NFO / ID3 / sidecar data
# (images, lyrics, subtitles) instead of synthetic fixtures.
#
#   - Read-only on the NAS. Writes ONLY into ./fulldata (gitignored).
#   - Streams over `tar`-over-`ssh` (system binaries only — no GNU rsync /
#     Homebrew dependency, and handles spaces/unicode in names natively).
#   - Stays lean (~2.5 GB) by excluding pre-baked .trickplay sprites and the
#     trailers/featurettes/other extras — none of which the scanner needs.
#   - NOT resumable (a re-run re-streams everything); it's a one-shot seed.
#
# Override the SSH host with NAS_HOST=othername if your alias differs.
#
set -euo pipefail

NAS="${NAS_HOST:-nas}"
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10)
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="$ROOT/fulldata"

# pull <remote-base> <dest-subdir> <remote-find-command>
# Runs the find on the NAS, tars the selected files (paths relative to the
# remote base), streams the archive back over ssh, and extracts under DEST.
pull() {
  local base="$1" sub="$2" findcmd="$3"
  mkdir -p "$DEST/$sub"
  "${SSH[@]}" "$NAS" "cd \"$base\" && { $findcmd; } | tar -cf - -T -" | tar -xf - -C "$DEST/$sub"
}

echo "==> Movies: A Goofy Movie (1995)   (~0.8 GB; movie.nfo + {imdb-...} in filename + .srt, excl. trickplay)"
pull "/storage/Movies/Foreign" "Movies" \
  'find "A Goofy Movie (1995)" -type f ! -path "*.trickplay/*"'

echo "==> Music: 3 Doors Down            (artist.nfo + art + 1 album + 2 singles; flac/mp3/m4a + .lrc, ~0.7 GB)"
pull "/storage/Music" "Music" '
  find "3 Doors Down" -maxdepth 1 -type f \( -iname "*.nfo" -o -iname "*.jpg" -o -iname "*.png" \)
  find "3 Doors Down/3 Doors Down - Album - 2000 - The Better Life" -type f
  find "3 Doors Down/3 Doors Down - Single - 2000 - Kryptonite" -type f
  find "3 Doors Down/3 Doors Down - Single - 2007 - Citizen Soldier" -type f'

echo "==> TV: 3 Body Problem (2024)      (tvshow.nfo + show art + theme + S01E01-E02 mkv/nfo/thumb, excl. extras+trickplay, ~1.0 GB)"
pull "/storage/TV/Foreign" "TV" '
  find "3 Body Problem (2024)" -maxdepth 1 -type f
  find "3 Body Problem (2024)/Season 01" -type f \( -iname "*S01E01*" -o -iname "*S01E02*" \) ! -path "*.trickplay/*"'

echo ""
echo "Done. Subset sizes:"
du -sh "$DEST"/Music "$DEST"/TV "$DEST"/Movies 2>/dev/null || true
