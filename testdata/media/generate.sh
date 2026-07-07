#!/usr/bin/env bash
#
# Generate the synthetic media test corpus under testdata/media/.
#
# These are *real, playable* files intended to exercise probe / transcode /
# playback / sonic paths. The testdata/library/ fixture symlinks into them.
# Everything is synthetic — solid tones, noise, and a static "test" card — so
# nothing here is copyrighted.
#
# Reproducible and idempotent: safe to re-run; it overwrites its outputs.
#
# Requirements: ffmpeg (libx264/libx265/libsvtav1/flac/libmp3lame/libopus,
# all present in the Homebrew build) and ImageMagick (`magick`) for the text
# card, since this ffmpeg is built without libfreetype/drawtext.
#
# Caveats:
#   * .ogg holds real Vorbis, encoded by oggenc (vorbis-tools) fed WAV over a
#     pipe — this ffmpeg has no libvorbis and its native experimental vorbis
#     encoder emits no packets, so oggenc is required.
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AUDIO_DIR="$HERE/audio"
VRES_DIR="$HERE/video/res"
VCODEC_DIR="$HERE/video/codec"
mkdir -p "$AUDIO_DIR" "$VRES_DIR" "$VCODEC_DIR"

DUR=120                       # 2 minutes
SR=48000
BLUE="#1f6feb"
FONT="/System/Library/Fonts/Supplemental/Arial Bold.ttf"
# -nostdin: the resolution loop reads a heredoc via `read`; without this,
# ffmpeg would consume that stdin and mangle the loop.
FF="ffmpeg -hide_banner -loglevel error -nostdin -y"

FRAMES="$(mktemp -d)"
trap 'rm -rf "$FRAMES"' EXIT

# --- audio source expression for a given noise type ------------------------
noise_src() {
  case "$1" in
    sine)   echo "sine=frequency=440:sample_rate=$SR:duration=$DUR,volume=0.3" ;;
    brown)  echo "anoisesrc=color=brown:sample_rate=$SR:duration=$DUR:amplitude=0.3" ;;
    purple) echo "anoisesrc=color=violet:sample_rate=$SR:duration=$DUR:amplitude=0.3" ;;
    pink)   echo "anoisesrc=color=pink:sample_rate=$SR:duration=$DUR:amplitude=0.3" ;;
    *) echo "unknown noise: $1" >&2; return 1 ;;
  esac
}

# --- encode one audio file: <noise> <ext> ----------------------------------
gen_audio() {
  noise="$1"; ext="$2"; out="$AUDIO_DIR/$noise.$ext"; src="$(noise_src "$noise")"
  title="$noise ($ext)"
  # Vorbis has no ffmpeg encoder here, so stream WAV into oggenc.
  if [ "$ext" = ogg ]; then
    $FF -f lavfi -i "$src" -f wav - \
      | oggenc -Q -q 5 -t "$title" -a "Heya Test Tones" -l "Test Corpus" \
          -G "Test" -d "2026" -o "$out" -
    echo "  audio  $out"
    return
  fi
  case "$ext" in
    mp3)  set -- -c:a libmp3lame -b:a 192k ;;
    m4a)  set -- -c:a aac        -b:a 192k ;;
    flac) set -- -c:a flac ;;
    *) echo "unknown ext: $ext" >&2; return 1 ;;
  esac
  $FF -f lavfi -i "$src" "$@" \
    -metadata title="$title" \
    -metadata artist="Heya Test Tones" \
    -metadata album="Test Corpus" \
    -metadata genre="Test" \
    -metadata date="2026" \
    "$out"
  echo "  audio  $out"
}

# --- encode one video: <out> <w> <h> <codec args...> -----------------------
gen_video() {
  out="$1"; w="$2"; h="$3"; shift 3
  fs=$(( h / 4 ))
  frame="$FRAMES/frame_${w}x${h}.png"
  if [ ! -f "$frame" ]; then
    magick -size "${w}x${h}" "xc:$BLUE" -font "$FONT" -pointsize "$fs" \
      -fill white -gravity center -annotate 0 'test' "$frame"
  fi
  $FF -loop 1 -framerate 24 -i "$frame" \
      -f lavfi -i "sine=frequency=440:sample_rate=$SR:duration=$DUR,volume=0.1" \
      -t "$DUR" -map 0:v -map 1:a \
      "$@" \
      -pix_fmt yuv420p -r 24 -g 48 \
      -c:a aac -b:a 128k -movflags +faststart \
      "$out"
  echo "  video  $out"
}

# --- minimal valid EPUB (zip with stored mimetype first) -------------------
mk_epub() {
  out="$1"; tmp="$(mktemp -d)"
  printf 'application/epub+zip' > "$tmp/mimetype"
  mkdir -p "$tmp/META-INF" "$tmp/OEBPS"
  cat > "$tmp/META-INF/container.xml" <<'XML'
<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>
XML
  cat > "$tmp/OEBPS/content.opf" <<'XML'
<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:heya-test-book</dc:identifier>
    <dc:title>Test Book</dc:title>
    <dc:creator>Heya Test</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest><item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>
  <spine><itemref idref="c1"/></spine>
</package>
XML
  printf '%s' "<?xml version='1.0'?><html xmlns='http://www.w3.org/1999/xhtml'><body><p>Test book.</p></body></html>" > "$tmp/OEBPS/c1.xhtml"
  rm -f "$out"
  ( cd "$tmp" && zip -X0 -q "$out" mimetype && zip -Xr9 -q "$out" META-INF OEBPS )
  rm -rf "$tmp"
}

echo "==> audio (4 noise types x 4 formats)"
for noise in sine brown purple pink; do
  for ext in mp3 m4a flac ogg; do
    gen_audio "$noise" "$ext"
  done
done

echo "==> video by resolution (h264)"
while read -r name w h; do
  [ -z "$name" ] && continue
  gen_video "$VRES_DIR/test-$name.mp4" "$w" "$h" -c:v libx264 -preset veryfast -crf 23
done <<'EOF'
480p   854  480
720p   1280 720
1080p  1920 1080
1440p  2560 1440
2160p  3840 2160
EOF

echo "==> video by codec (720p)"
gen_video "$VCODEC_DIR/test-h264.mp4" 1280 720 -c:v libx264   -preset veryfast -crf 23
gen_video "$VCODEC_DIR/test-hevc.mp4" 1280 720 -c:v libx265   -preset veryfast -crf 28 -tag:v hvc1
gen_video "$VCODEC_DIR/test-av1.mp4"  1280 720 -c:v libsvtav1 -preset 8        -crf 35

echo "==> library extras (aac tones + minimal pdf/epub)"
BOOKS_DIR="$HERE/books"; mkdir -p "$BOOKS_DIR"
for n in sine brown; do
  $FF -f lavfi -i "$(noise_src "$n")" -c:a aac -b:a 160k -f adts "$AUDIO_DIR/$n.aac"
  echo "  audio  $AUDIO_DIR/$n.aac"
done
magick -size 640x900 xc:white -font "$FONT" -pointsize 56 -fill black \
  -gravity center -annotate 0 $'TEST\nBOOK' "$BOOKS_DIR/test.pdf"
echo "  book   $BOOKS_DIR/test.pdf"
mk_epub "$BOOKS_DIR/test.epub"
echo "  book   $BOOKS_DIR/test.epub"

echo "==> done"
