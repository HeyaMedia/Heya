# testdata/media — synthetic playable media

Real, decodable audio/video for exercising probe / transcode / playback /
sonic-analysis paths. Everything is synthetic (tones, noise, a static "test"
card) so nothing here is copyrighted. Regenerate with [`generate.sh`](generate.sh).

The `testdata/library/` fixture symlinks into these files to build a realistic
library without duplicating bytes.

## Audio — `audio/` (2 min each, 48 kHz stereo)

4 noise types × 4 formats = 16 files, tagged `artist="Heya Test Tones"`,
`album="Test Corpus"`, `title="<noise> (<ext>)"`.

| noise    | source                          | mp3        | m4a  | flac | ogg          |
| -------- | ------------------------------- | ---------- | ---- | ---- | ------------ |
| `sine`   | 440 Hz sine                     | libmp3lame | aac  | flac | Vorbis       |
| `brown`  | brown (red) noise               | libmp3lame | aac  | flac | Vorbis       |
| `purple` | violet noise                    | libmp3lame | aac  | flac | Vorbis       |
| `pink`   | pink noise                      | libmp3lame | aac  | flac | Vorbis       |

Noise FLACs are ~13–14 MB (noise is incompressible); `sine.flac` and all the
lossy files are small.

## Video — `video/` (2 min each, 24 fps, h264 unless noted, quiet 440 Hz tone)

Blue field with a centered white **test** card.

`video/res/` — one resolution axis (all h264):
`test-480p` (854×480), `test-720p` (1280×720), `test-1080p` (1920×1080),
`test-1440p` (2560×1440), `test-2160p` (3840×2160).

`video/codec/` — one codec axis (all 720p):
`test-h264`, `test-hevc` (hvc1-tagged), `test-av1` (libsvtav1).

## Extras (for the library fixture)

- `audio/{sine,brown}.aac` — ADTS AAC, for the `audiobooks/` fixture.
- `books/test.pdf`, `books/test.epub` — minimal valid PDF/EPUB, for the `books/` fixture.

## Notes / caveats

- **Container vs. extension:** all video is `.mp4`. If a test needs `.mkv`,
  remux with stream copy — `ffmpeg -i in.mp4 -c copy out.mkv` — it's instant.
- **`.ogg` is real Vorbis**, encoded by `oggenc` (vorbis-tools) fed WAV over a
  pipe. This ffmpeg build has no `libvorbis` and its native experimental
  `vorbis` encoder emits no packets, so `oggenc` is required to regenerate.
- **No `drawtext`:** this ffmpeg lacks libfreetype, so the text card is
  rendered by ImageMagick (`magick`) and encoded as a looped still.
- Regenerating requires `ffmpeg`, `oggenc`, and `magick` on `PATH`.
