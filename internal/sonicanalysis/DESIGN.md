# Sonic Analysis — Design

Integration plan for promoting the music-analysis proof-of-concept
(in `cmd/sonic-poc/`) into Heya proper. This document is the single
source of truth for the integration; it survives context compaction
and serves as design history afterwards.

---

## 1. Goals

Produce a complete per-track "sonic profile" stored in Postgres
that powers:

- **Sonic similarity** at three levels: track, artist, album
- **Natural-language search** ("epic cinematic battle music") via
  CLAP audio↔text shared embedding space
- **Mood / danceability / voice / 400-genre tagging**
- **BPM, musical key, EBU R128 loudness, true peak**
- **Playbar waveform** rendering
- **Derived features** (no extra storage): instant playlists, radio
  from track, song-path (A → B transition bridge), song-alchemy
  (vector arithmetic), sonic-fingerprint (per-user listening
  history aggregate), mood-filtered playlists, harmonic mixing,
  loudness-normalized playback

All extraction runs **on the Heya server** as a pure-Go pipeline
(no Python runtime, no external service dependency), bundled with
the binary. Hardware acceleration auto-detected per platform
(CoreML / CUDA / DirectML / CPU fallback).

## 2. Non-goals (v1)

Deferred to `future.md`:

- **Music map** (UMAP 2D projection) — needs scheduler post-pass
- **Lyrics fetch + search** — needs HeyaMedia `lrclib` integration
- **MERT model swap** — CC-BY-NC license blocker
- **Per-segment embeddings** — 3× storage + different KNN topology

## 3. Constraints (user-confirmed decisions)

| Decision | Choice |
|---|---|
| Model lifecycle | Loaded by scheduler at window open, **unloaded at window close** |
| Model distribution | **Async download at server startup**, status surfaced via API |
| Centroid maintenance | **End-of-batch refresh** (not per-track incremental) |
| Time window enforcement | In-flight track finishes; **no new track after `end_hour`** |
| Music import → analysis | **Always scheduled**, never run immediately |
| Concurrency | **Configurable CPU preparation-ahead and GPU lanes**; OpenVINO inference is serialized in-process because concurrent sessions crash its execution provider |
| Key schema | **Smallints** (0=C, 0=major) + Go enum types (`PitchClass`, `KeyMode`) |
| Migration strategy | **New numbered migration**, additive only, no edits to existing |
| Progress UI | **Existing WebSocket eventhub** (same path trickplay uses) |
| Search lifecycle | CLAP text encoder lives in **`TextSearcher`** (separate from analysis Analyzer), **lazy-loaded on first search**, kept warm |

## 4. PoC-validated facts

Production numbers from full 454-track library on M-series Mac (CoreML
for fixed-batch heads, CPU for dynamic-batch heads + DSP, ffmpeg for
loudness):

| Per-track cost | Time |
|---|---|
| ffmpeg shared 16/48 kHz decode | ~400 ms |
| Mel-spec preprocessing (96 bands @ 16 kHz) | ~600 ms |
| CLAP-specific mel-spec (64 bands @ 48 kHz, 3 × 10 s) | ~600 ms |
| Discogs track_embeddings inference (CoreML) | ~50 ms total |
| Discogs artist_embeddings inference (CoreML) | ~50 ms total |
| Discogs release_embeddings inference (CoreML) | ~50 ms total |
| Base EffNet + 9 classifier heads (CPU, dynamic batch) | ~1.9 s total |
| CLAP audio encoder (3 windows) | ~2.4 s |
| BPM (onset+autocorr) | ~1.1 s |
| Key (chromagram + K-S) | ~0.8 s |
| Loudness (ffmpeg ebur128) | ~1.3 s |
| Waveform + boundaries (shared 16 kHz PCM) | ~0.1 s |

The table records the original PoC ballpark. The production pipeline overlaps
the CPU stages, prepares tracks ahead of inference, and shares one source
decode, so these rows must not be summed to predict current wall time.

CoreML graph-compile cost (paid on first Analyzer.Load):

- discogs_track_embeddings: 1.5 s
- discogs_artist_embeddings: 1.5 s
- discogs_release_embeddings: 1.5 s
- CLAP audio: 5.8 s
- Total cold load: ~10-15 s

Storage per track: ~85 KB (4 vectors × 512 × float32 = 8 KB + 2000-bucket
waveform × float32 = 8 KB + JSON tags ~5 KB + metadata).

Quality validated qualitatively:

- Top-10 track similarity: Charlotte de Witte techno → all Charlotte de Witte techno
- Top-10 album similarity: 18 Months (2012 EDM) → all his 2009-2014 big-room EDM era
- Cross-artist album similarity: Funk Wav Bounces Vol. 1 → bleeds into Charli XCX Brat (both chill pop)
- Artist similarity Calvin Harris ↔ Charli XCX = 0.91 (mutually closest), Charlotte de Witte outlier at 0.50-0.63
- CLAP text "hard dark industrial techno" → top 10 = all Charlotte de Witte techno
- CLAP text "intense aggressive vocal rock with female screaming" → top 10 = all Ado

## 5. Schema

Standalone migration `migrations/000XX_track_facets.sql` (next free
number — likely 00006 or 00007; verify against `migrations/` at
implementation). Additive only; no edits to existing migrations.

```sql
-- +goose Up

CREATE EXTENSION IF NOT EXISTS vector;

-- ===========================================================
-- Per-track facets: vectors + DSP measurements + waveform
-- ===========================================================
CREATE TABLE track_facets (
    music_track_id    bigint PRIMARY KEY REFERENCES music_tracks(id) ON DELETE CASCADE,

    -- Sonic embeddings (Discogs specialized heads + CLAP)
    track_embedding   vector(512),   -- discogs_track_embeddings
    artist_embedding  vector(512),   -- discogs_artist_embeddings
    release_embedding vector(512),   -- discogs_release_embeddings
    text_embedding    vector(512),   -- normalized mean of CLAP 20/50/80% windows
    clap_windows      smallint,      -- persisted coverage; current value is 3

    -- DSP-derived facets
    bpm                real,         -- 50..200, NULL if no clear beat
    bpm_confidence     real,         -- 0..1
    key_root           smallint,     -- 0=C, 1=C#, ..., 11=B
    key_mode           smallint,     -- 0=major, 1=minor
    key_clarity        real,         -- 0..1, gap to second-best K-S match
    integrated_lufs    real,         -- EBU R128 integrated loudness
    loudness_range_lu  real,         -- LRA (dynamic range)
    true_peak_dbtp     real,         -- can exceed 0 (intersample clipping)

    -- Tag outputs
    top_genres         jsonb,        -- [{"name":"Electronic---Techno","score":0.67}, ...]
    mood_tags          jsonb,        -- {"danceability":0.99,"mood_happy":0.42,...}

    -- Visualization
    waveform           real[],       -- 2000 peak buckets [0..1]

    analyzed_at        timestamptz NOT NULL DEFAULT now(),
    analyzer_version   int NOT NULL DEFAULT 1
);

-- ===========================================================
-- Pre-aggregated artist + album centroids — refreshed at the end
-- of each analyzer window batch. Live in dedicated tables (not
-- materialized views) so we can incrementally UPSERT them.
-- ===========================================================
CREATE TABLE artist_centroids (
    music_artist_id  bigint PRIMARY KEY REFERENCES music_artists(id) ON DELETE CASCADE,
    sonic_centroid   vector(512),   -- avg of artist_embedding across this artist's tracks
    text_centroid    vector(512),   -- avg of text_embedding across this artist's tracks
    track_count      int  NOT NULL DEFAULT 0,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE album_centroids (
    music_album_id   bigint PRIMARY KEY REFERENCES music_albums(id) ON DELETE CASCADE,
    sonic_centroid   vector(512),   -- avg of release_embedding
    text_centroid    vector(512),
    track_count      int  NOT NULL DEFAULT 0,
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- HNSW indexes for fast KNN
CREATE INDEX track_facets_track_emb_hnsw   ON track_facets    USING hnsw (track_embedding   vector_cosine_ops);
CREATE INDEX track_facets_text_emb_hnsw    ON track_facets    USING hnsw (text_embedding    vector_cosine_ops);
CREATE INDEX artist_centroids_sonic_hnsw   ON artist_centroids USING hnsw (sonic_centroid   vector_cosine_ops);
CREATE INDEX album_centroids_sonic_hnsw    ON album_centroids  USING hnsw (sonic_centroid   vector_cosine_ops);

-- Index for "next track to analyze" lookup
CREATE INDEX track_facets_version_idx ON track_facets (analyzer_version);

-- +goose Down
DROP TABLE IF EXISTS album_centroids;
DROP TABLE IF EXISTS artist_centroids;
DROP TABLE IF EXISTS track_facets;
```

**Verify at implementation time:** actual table names in current
schema (`music_tracks`, `music_artists`, `music_albums` — confirm
exact names from existing migrations).

## 6. Package layout

```
internal/sonicanalysis/
├── DESIGN.md           # this file
├── analyzer.go         # Analyzer struct + state machine + Load/Unload
├── facets.go           # Facets struct (output of one Analyze call)
├── fetcher.go          # ModelFetcher — async download at boot
├── musictheory.go      # PitchClass, KeyMode, Key, Camelot helpers
├── audio.go            # ffmpeg PCM decode (configurable sample rate)
├── mel.go              # Discogs/MusiCNN mel-spec (96 bands @ 16 kHz)
├── clap_mel.go         # CLAP mel-spec (64 bands @ 48 kHz, 10 s clip)
├── extractor.go        # patch slicing, batch padding, mean pooling
├── onnx.go             # ORT init, EP selection (buildSessionOptions)
├── discogs_heads.go    # head bank (track/artist/release) — bs64 fixed batch
├── effnet_base.go      # base EffNet (genre + 1280-dim) — dynamic batch
├── classifiers.go      # 9 mood/dance/voice classifier heads
├── clap_audio.go       # CLAP audio encoder
├── clap_text.go        # CLAP text encoder + sugarme tokenizer
├── bpm.go              # spectral-flux ODF + autocorrelation
├── key.go              # Krumhansl-Schmuckler chromagram match
├── loudness.go         # ffmpeg ebur128 stderr parse
├── waveform.go         # 8 kHz decode + 2000 peak buckets
├── progress.go         # eventhub event publishers
└── *_test.go           # unit tests where applicable
```

```
internal/sonicanalysis/text_search.go
                       # TextSearcher: lives separately from Analyzer;
                       # owns CLAP text encoder + tokenizer; lazy-load
                       # on first query, stays warm

internal/scheduler/sonicanalysis.go
                       # Job that the scheduler ticks; runBatch() is
                       # the long-running window-bound loop

internal/service/music_facets.go
                       # FacetsService — read API for similarity,
                       # search, derived playlists, sonic fingerprint

internal/server/music_facets_handlers.go
                       # Huma v2 endpoint handlers

cmd/heya/cmd/analyze.go
                       # CLI: status, run --once, reset, fetch-models,
                       # warmup
```

PoC code in `cmd/sonic-poc/` is **deleted entirely** in Phase 1c.
The Go code is the same logic ported into `package sonicanalysis`;
no functional changes during the port.

## 7. Analyzer lifecycle

```go
type AnalyzerState int32

const (
    StateUnloaded AnalyzerState = iota
    StateLoading
    StateReady
    StateUnloading
)

type Analyzer struct {
    cfg     Config
    bundle  *modelBundle           // nil when not loaded
    state   atomic.Int32           // AnalyzerState
    log     zerolog.Logger
}

// Load opens every analysis ONNX session in sequence:
//   - 3× Discogs heads (track/artist/release)
//   - Base EffNet (genre + 1280-dim)
//   - 9× classifier heads (mood/dance/voice)
//   - CLAP audio encoder
// 5-15 s cold (CoreML compilation), <1 s warm if ORT caches compiled
// models. Errors if models are missing on disk → caller (scheduler)
// should mark "waiting for models" and retry next window.
func (a *Analyzer) Load(ctx context.Context) error

// Unload destroys every session, freeing ~700 MB resident memory.
// Called at end of window batch or on server shutdown.
func (a *Analyzer) Unload()

// Analyze runs the full per-track pipeline. Returns ErrAnalyzerNotReady
// if state != Ready.
func (a *Analyzer) Analyze(ctx context.Context, path string) (*Facets, error)

// State / IsReady accessors for the scheduler + status endpoint.
func (a *Analyzer) State() AnalyzerState
```

```go
type Facets struct {
    TrackEmbed    []float32  // 512
    ArtistEmbed   []float32  // 512
    ReleaseEmbed  []float32  // 512
    TextEmbed     []float32  // 512  (CLAP audio side)

    BPM           float64
    BPMConfidence float64
    Key           Key        // {Root PitchClass, Mode KeyMode}
    KeyClarity    float64

    Loudness      Loudness   // {Integrated, Range, TruePeak}

    TopGenres     []GenreScore  // top-N from 400-class softmax
    MoodTags      MoodScores    // typed map of P(positive) per head

    Waveform      []float32  // 2000 peaks [0..1]

    ElapsedMs     int        // total analyze() wall time
}
```

## 8. Scheduler design

Lives in `internal/scheduler/sonicanalysis.go` alongside trickplay
and thumbnails. The existing scheduler's tick cadence (likely ~1
min) drives the entry point.

```go
type Job struct {
    cfg      Config
    analyzer *sonicanalysis.Analyzer
    queries  *sqlc.Queries
    events   *eventhub.Hub
    running  atomic.Bool          // in-process mutex: one batch at a time
    log      zerolog.Logger
}

// Tick is called by the scheduler every N seconds. Returns quickly;
// the heavy work happens in a separate goroutine.
func (j *Job) Tick(ctx context.Context) error {
    if j.running.Load() { return nil }              // already batching
    if !j.cfg.Window.ContainsNow() { return nil }   // outside window
    if !j.hasPending(ctx) { return nil }            // nothing to do
    if !j.analyzer.ModelsReady() { return nil }     // fetcher not done

    go j.runBatch(ctx)
    return nil
}

func (j *Job) runBatch(parent context.Context) {
    j.running.Store(true)
    defer j.running.Store(false)

    // Window enforcement: use a context that hard-cancels at window end.
    // The in-flight Analyze call will keep running (typically ~10 s)
    // — the cancellation just prevents the next track from starting.
    ctx, cancel := context.WithDeadline(parent, j.cfg.Window.NextEnd())
    defer cancel()

    if err := j.analyzer.Load(ctx); err != nil {
        j.events.Publish("sonicanalysis.load_failed", err)
        return
    }
    defer j.analyzer.Unload()
    j.events.Publish("sonicanalysis.window_opened", j.summary(ctx))

    affected := newAffectedSets()
    for ctx.Err() == nil && j.cfg.Window.ContainsNow() {
        next, err := j.queries.NextTrackForAnalysis(ctx, j.cfg.CurrentVersion)
        if errors.Is(err, sql.ErrNoRows) { break }
        if err != nil { j.log.Err(err).Msg("next track"); break }

        facets, aErr := j.analyzer.Analyze(ctx, next.Path)
        if aErr != nil {
            j.markFailed(ctx, next.ID, aErr)
            continue
        }
        if pErr := j.persist(ctx, next, facets); pErr != nil { continue }
        affected.add(next.ArtistID, next.AlbumID)
        j.events.Publish("sonicanalysis.track_completed", ...)
    }

    j.refreshCentroids(ctx, affected)        // single batched update
    j.events.Publish("sonicanalysis.window_closed", j.summary(ctx))
}
```

**Event payloads** published to eventhub for the frontend:

| Event | Payload |
|---|---|
| `sonicanalysis.window_opened` | `{queue_depth, models_state}` |
| `sonicanalysis.track_started` | `{track_id, path, queue_depth, completed_this_window}` |
| `sonicanalysis.track_completed` | `{track_id, elapsed_ms, queue_depth, completed_this_window}` |
| `sonicanalysis.track_failed` | `{track_id, error}` |
| `sonicanalysis.window_closed` | `{completed, failed, elapsed_seconds}` |
| `sonicanalysis.load_failed` | `{error}` |

Frontend subscribes through the existing WS endpoint; status UI
shows progress bar + "N of M tracks analyzed this window".

**Window enforcement detail.** Use `context.WithDeadline(parent,
nextEnd)` for the per-batch context. Pass that context into
`Analyzer.Analyze()`. When the deadline hits:
- The in-flight track's ffmpeg subprocess will be killed promptly
  (ffmpeg cancellation works mid-decode).
- ONNX inference is non-context-aware but each call is ≤200 ms, so
  the post-cancel cleanup is fast.
- Per user-confirmed decision: we *let the current track finish*.
  This means the deadline only blocks the *next iteration* of the
  loop, not the current one. Adjust: don't pass `ctx` to
  `Analyze`; pass `parent` (no deadline). Check `j.cfg.Window.ContainsNow()`
  in the loop condition.

Updated loop:
```go
for j.cfg.Window.ContainsNow() && parent.Err() == nil {
    next, err := j.queries.NextTrackForAnalysis(parent, ...)
    ...
    facets, err := j.analyzer.Analyze(parent, next.Path)  // not ctx, so window end doesn't interrupt
    ...
}
```

Where `parent` is the server lifecycle context (cancelled only on
shutdown), and the window check at top of loop prevents the next
iteration. Server shutdown still interrupts the in-flight track
cleanly via parent.

## 9. ModelFetcher

```go
type ModelFile struct {
    Name    string  // e.g. "discogs_track_embeddings-effnet-bs64-1.onnx"
    SubPath string  // relative path inside ModelsDir, e.g. "discogs/" or "clap/"
    URL     string
    SHA256  string  // for integrity check (populated from known-good values)
    Size    int64   // bytes
}

type FetcherState int32
const (
    FetcherIdle FetcherState = iota
    FetcherChecking
    FetcherFetching
    FetcherReady
    FetcherFailed
)

type ModelFetcher struct {
    targetDir string
    manifest  []ModelFile
    state     atomic.Int32
    progress  atomic.Pointer[FetchProgress]
    err       atomic.Pointer[error]
    log       zerolog.Logger
}

type FetchProgress struct {
    CurrentFile  string
    BytesDone    int64
    BytesTotal   int64
    FilesDone    int
    FilesTotal   int
}

// Run is invoked by the server at startup in a background goroutine.
// Verifies each file exists + has correct SHA256; downloads any
// missing. Sets state to Ready when all files are present + verified.
func (f *ModelFetcher) Run(ctx context.Context) error
```

**Manifest** (compile-time constant, sourced from URLs we already
validated during the PoC):

```go
var DefaultManifest = []ModelFile{
    {Name: "discogs_track_embeddings-effnet-bs64-1.onnx", URL: ".../discogs-effnet/discogs_track_embeddings-effnet-bs64-1.onnx", Size: 19_000_000},
    {Name: "discogs_artist_embeddings-effnet-bs64-1.onnx", URL: ".../discogs-effnet/discogs_artist_embeddings-effnet-bs64-1.onnx", Size: 19_000_000},
    {Name: "discogs_release_embeddings-effnet-bs64-1.onnx", URL: ".../discogs-effnet/discogs_release_embeddings-effnet-bs64-1.onnx", Size: 19_000_000},
    {Name: "discogs-effnet-bsdynamic-1.onnx", URL: ".../discogs-effnet/discogs-effnet-bsdynamic-1.onnx", Size: 18_000_000},
    {Name: "discogs-effnet-bsdynamic-1.json", URL: ".../discogs-effnet/discogs-effnet-bsdynamic-1.json", Size: 15_000}, // 400 genre names
    // 9× classifier heads under heads/, ~514 KB each
    {Name: "heads/danceability-discogs-effnet-1.onnx", URL: ".../classification-heads/danceability/danceability-discogs-effnet-1.onnx", Size: 514_000},
    // ... mood_happy, mood_sad, mood_aggressive, mood_relaxed, mood_party, mood_electronic, mood_acoustic, voice_instrumental
    // CLAP
    {Name: "clap/audio_model.onnx", URL: "huggingface.co/Xenova/clap-htsat-unfused/resolve/main/onnx/audio_model.onnx", Size: 118_000_000},
    {Name: "clap/text_model.onnx", URL: "huggingface.co/Xenova/clap-htsat-unfused/resolve/main/onnx/text_model.onnx", Size: 502_000_000},
    {Name: "clap/tokenizer.json", URL: "huggingface.co/Xenova/clap-htsat-unfused/resolve/main/tokenizer.json", Size: 2_100_000},
    {Name: "clap/merges.txt", URL: ".../merges.txt", Size: 456_000},
    {Name: "clap/vocab.json", URL: ".../vocab.json", Size: 798_000},
    {Name: "clap/special_tokens_map.json", URL: ".../special_tokens_map.json", Size: 280},
}
```

Total download: ~720 MB. Resumable downloads (range requests) are a
nice-to-have, not v1.

Container builds COPY the models directory at image build time so
new containers start without re-downloading. The fetcher is mainly
for dev / first-run / users who use a non-image deployment.

## 10. Music theory enums

```go
// internal/sonicanalysis/musictheory.go

type PitchClass int8

const (
    PitchC PitchClass = iota
    PitchCsharp                    // also Db
    PitchD
    PitchDsharp                    // also Eb
    PitchE
    PitchF
    PitchFsharp                    // also Gb
    PitchG
    PitchGsharp                    // also Ab
    PitchA
    PitchAsharp                    // also Bb
    PitchB
)

func (p PitchClass) String() string {
    return [...]string{"C","C#","D","D#","E","F","F#","G","G#","A","A#","B"}[p]
}
func (p PitchClass) Flat() string { ... }

type KeyMode int8
const (
    KeyModeMajor KeyMode = iota
    KeyModeMinor
)
func (m KeyMode) String() string { return [...]string{"major","minor"}[m] }

type Key struct {
    Root PitchClass
    Mode KeyMode
}
func (k Key) String() string { return k.Root.String() + " " + k.Mode.String() }
func (k Key) CamelotCode() string  // "8B" for C major, "5A" for C minor, etc.
func (k Key) IsHarmonicallyCompatible(other Key) bool  // ±1 wheel step or relative

type MoodTagName string

const (
    MoodDanceability    MoodTagName = "danceability"
    MoodVoice           MoodTagName = "voice"
    MoodHappy           MoodTagName = "mood_happy"
    MoodSad             MoodTagName = "mood_sad"
    MoodAggressive      MoodTagName = "mood_aggressive"
    MoodRelaxed         MoodTagName = "mood_relaxed"
    MoodParty           MoodTagName = "mood_party"
    MoodElectronic      MoodTagName = "mood_electronic"
    MoodAcoustic        MoodTagName = "mood_acoustic"
)

type MoodScores map[MoodTagName]float32
```

## 11. Service & API

```go
// internal/service/music_facets.go

type FacetsService struct {
    q      *sqlc.Queries
    search *sonicanalysis.TextSearcher  // lazy-loaded CLAP text encoder
}

func (s *FacetsService) Facets(ctx, trackID) (*sonicanalysis.Facets, error)
func (s *FacetsService) Waveform(ctx, trackID) ([]float32, error)

func (s *FacetsService) SimilarTracks(ctx, trackID, limit) ([]TrackResult, error)
func (s *FacetsService) SimilarArtists(ctx, artistID, limit) ([]ArtistResult, error)
func (s *FacetsService) SimilarAlbums(ctx, albumID, limit) ([]AlbumResult, error)

func (s *FacetsService) Search(ctx, text, limit) ([]TrackResult, error)  // CLAP text

func (s *FacetsService) GenerateRadio(ctx, seedTrackID, length, diversity) ([]TrackResult, error)
func (s *FacetsService) SongPath(ctx, fromID, toID, steps) ([]TrackResult, error)
func (s *FacetsService) SongAlchemy(ctx, addIDs, subIDs, limit) ([]TrackResult, error)

func (s *FacetsService) SonicFingerprint(ctx, userID) ([]float32, error)
func (s *FacetsService) Recommendations(ctx, userID, limit) ([]TrackResult, error)
```

**HTTP API** (Huma v2):

| Method | Path | Returns |
|---|---|---|
| GET | /api/tracks/{slug}/facets | full Facets bundle (excl. waveform for compactness) |
| GET | /api/tracks/{slug}/waveform | `[]float32` (2000) |
| GET | /api/tracks/{slug}/similar | top-N tracks by `track_embedding` cosine |
| GET | /api/artists/{slug}/similar | top-N artists by `artist_centroids.sonic_centroid` |
| GET | /api/albums/{slug}/similar | top-N albums by `album_centroids.sonic_centroid` |
| GET | /api/search?q={text}&type=audio | CLAP text→audio KNN |
| POST | /api/playlists/radio | `{seed_track_id, length, diversity}` → ordered tracklist |
| POST | /api/playlists/song-path | `{from_id, to_id, steps}` → bridge sequence |
| POST | /api/playlists/song-alchemy | `{add_ids, sub_ids, limit}` → vector-arith results |
| GET | /api/me/recommendations | sonic fingerprint → top-N tracks |
| GET | /api/admin/sonicanalysis/status | fetcher state + analyzer state + scheduler state + queue depth |

## 12. CLI (`cmd/heya/cmd/analyze.go`)

```
heya analyze status              # progress + window + ETA + models state
heya analyze run --once          # one-shot: load, analyze N=1, unload, exit
heya analyze run --until=4h      # run until duration elapses (ignore window)
heya analyze reset [--library=X] # bump analyzer_version, force redo
heya analyze fetch-models        # blocking download (dev workflow)
heya analyze warmup              # load models, run inference on /dev/zero, prove works
```

## 13. Frontend changes

### Music page (track detail)
- BPM + key + LUFS chips (small inline badges)
- Mood tag pills (clickable → "more like this")
- Top-genre chips
- "Similar tracks" rail
- "Radio" button next to play

### Album page
- Add "Similar albums" rail

### Artist page
- Add "Similar artists" rail (combine with Layer 1's LB+Last.fm graph similarity for best-of-both)

### Playbar
- Canvas waveform render (`<audio>` element fed from `/api/tracks/{slug}/waveform`)
- Color-fill as `currentTime / duration` progresses
- Click-to-seek
- ~30-40 lines of Vue + canvas drawing

### Search
- Toggle: "Metadata" (existing) vs "Audio vibe" (new)
- "Audio vibe" search routes through `/api/search?type=audio`
- Optional: suggested prompts on focus ("hard techno", "chill jazz", "epic battle music")

### Admin → Sonic Analysis
- Settings page exposing `heya.yaml` window config (read-only initially, editable later)
- Models state indicator (fetching/ready, progress bar during fetch)
- Manual "trigger now" button (POST to admin endpoint)
- Live progress when in window (subscribes to WS events)

## 14. Config (`heya.yaml`)

```yaml
sonicanalysis:
  enabled: true
  models_dir: data/models/          # gitignored; container ships pre-populated
  accelerator: auto                 # auto|cpu|coreml|cuda|directml
  current_version: 1                # bump to force re-analysis library-wide
  poll_interval_seconds: 60         # scheduler tick when idle
  window:
    start_hour: 2                   # local-time hour, inclusive
    end_hour: 6                     # local-time hour, exclusive
    timezone: ""                    # "" = server local; else IANA name
  fetch_models_at_startup: true
  fetch_url_base: "https://essentia.upf.edu/models/"  # for Discogs/heads
```

## 15. PoC retirement

After Phase 1c lands (PoC code is fully promoted to internal):

- Delete `cmd/sonic-poc/` directory entirely
- Remove `cmd/sonic-poc/` lines from `.gitignore`
- Archive (or delete) `data/sonic-poc/` — PoC test data, not needed
  in production
- Update any references in `README.md` if they exist

Models live in `data/models/` (still gitignored under `data/`).

## 16. Phased execution

Each phase produces a shipable commit; nothing blocks the next phase
beyond what's listed.

| Phase | What | Blocks |
|---|---|---|
| 1a | Migration | 1b, 2 |
| 1b | sqlc queries + regen | 2 |
| 1c | Promote PoC → internal/sonicanalysis/ + musictheory.go | 2, 3 |
| 2 | service/music_facets.go (Search via TextSearcher) | 4a |
| 3a | ModelFetcher | 3b |
| 3b | Scheduler job + Analyzer Load/Unload | 4 |
| 4a | HTTP API | 5 |
| 4b | CLI commands | — |
| 5a | Music page facets + similarity rails | — |
| 5b | Playbar waveform + Radio button | — |
| 5c | CLAP text search UI | — |
| Cleanup | Delete cmd/sonic-poc/ | — |

Suggested grouping:
- **Session 1**: Phase 1 (migration + sqlc + package promotion)
- **Session 2**: Phase 2 + Phase 3 (service + analyzer + scheduler + fetcher)
- **Session 3**: Phase 4 (API + CLI)
- **Session 4**: Phase 5 (frontend)
- **Wrap**: Cleanup + final smoke test

## 17. Open items to verify at implementation time

When picking up this plan after a context reset:

- [ ] Actual music table names in `migrations/` — `music_tracks`?
      `music_artists`? `music_albums`? Match exactly.
- [ ] Existing scheduler API in `internal/scheduler/` — how
      trickplay/thumbnails register themselves; mirror that pattern.
- [ ] Existing eventhub API in `internal/eventhub/` — payload
      conventions; how to subscribe from the frontend.
- [ ] Next free migration number (likely 00006 or 00007).
- [ ] Existing Go module path: `github.com/karbowiak/heya` —
      already confirmed.
- [ ] How `heya.yaml` is loaded today (`internal/config/`) — add
      sonicanalysis section to the schema there.
- [ ] Container build (Dockerfile) — where to COPY models from at
      build time.
- [ ] Existing rail / chip Vue components in `web/app/components/`
      to reuse for similarity rails + mood pills.

## 18. Reference: validated model files + URLs

Place these under `models_dir/` (mirrors the PoC's `cmd/sonic-poc/models/`
layout):

```
{models_dir}/
├── discogs_track_embeddings-effnet-bs64-1.onnx   # 19M — track similarity head
├── discogs_artist_embeddings-effnet-bs64-1.onnx  # 19M — artist similarity head
├── discogs_release_embeddings-effnet-bs64-1.onnx # 19M — album similarity head
├── discogs-effnet-bsdynamic-1.onnx               # 18M — base EffNet (genre + 1280-dim)
├── discogs-effnet-bsdynamic-1.json               # 15K — 400-class names metadata
├── heads/
│   ├── danceability-discogs-effnet-1.onnx        # 514K — danceability
│   ├── voice_instrumental-discogs-effnet-1.onnx  # 514K — voice (class 1 = positive)
│   ├── mood_happy-discogs-effnet-1.onnx          # 514K
│   ├── mood_sad-discogs-effnet-1.onnx            # 514K (class 1 = positive)
│   ├── mood_aggressive-discogs-effnet-1.onnx     # 514K
│   ├── mood_relaxed-discogs-effnet-1.onnx        # 514K (class 1 = positive)
│   ├── mood_party-discogs-effnet-1.onnx          # 514K (class 1 = positive)
│   ├── mood_electronic-discogs-effnet-1.onnx     # 514K
│   └── mood_acoustic-discogs-effnet-1.onnx       # 514K
└── clap/
    ├── audio_model.onnx                          # 118M — CLAP HTSAT audio encoder
    ├── text_model.onnx                           # 502M — CLAP RoBERTa text encoder (fp32; fp16 has ORT 1.26 bug)
    ├── tokenizer.json                            # 2.1M — RoBERTa BPE
    ├── merges.txt                                # 456K
    ├── vocab.json                                # 798K
    └── special_tokens_map.json                   # 280B
```

**Discogs base URL**: `https://essentia.upf.edu/models/feature-extractors/discogs-effnet/`
**Classifier heads base URL**: `https://essentia.upf.edu/models/classification-heads/{name}/`
**CLAP base URL**: `https://huggingface.co/Xenova/clap-htsat-unfused/resolve/main/`

## 19. Critical implementation notes (gotchas)

1. **Mel-spec preprocessing must match Essentia exactly.** See
   `mel.go` for the parameters. Pitfalls (validated during PoC):
   - Frame extraction uses `startFromZero=false` (zero-centered
     first frame, not standard librosa center=True)
   - Hann window is **symmetric** (denominator N-1), not periodic
   - `type=power` (squared magnitudes), not magnitudes
   - `unit_tri` normalization (slaney-style triangular area), not
     `unit_sum`
   - Log compression `log10(10000·x + 1)`, not standard log/dB
   - Cosine match to reference Python: ~0.99 with our current
     ffmpeg+swr resampler. Production container should add
     `--enable-libsoxr` for full bit-equivalent.

2. **CLAP preprocessing is DIFFERENT from Discogs.** See `clap_mel.go`:
   - 48 kHz, 64 mel bands, fmin=50, fmax=14000
   - Periodic Hann (denominator N)
   - center=True with reflect padding
   - `10·log10(max(S, 1e-10))` dB compression
   - Three deterministic 10 s views centered at 20%, 50%, and 80%;
     shorter views use cyclic repeat-padding
   - Each view is normalized, then the three embeddings are mean-pooled and
     normalized again
   - Output shape `(1, 1, 1001, 64)` for the ONNX input

3. **CoreML EP selection per-model.** Fixed-batch models
   (Discogs heads at bs64) get major CoreML speedup. Dynamic-batch
   models (base EffNet, classifier heads) actually run SLOWER on
   CoreML because the graph recompiles per call. Use CPU for those.
   Selection happens inside `buildSessionOptions(accel)` —
   accept a per-model hint or force CPU for known-dynamic models.

4. **Classifier head class-order is inconsistent.** Some heads
   have the positive class at index 0 (danceability, mood_happy,
   mood_aggressive, mood_electronic, mood_acoustic), others at
   index 1 (mood_sad, mood_relaxed, mood_party, voice_instrumental).
   Track per-head with explicit `PosIndex` in the `headSpec`.

5. **CLAP fp16 text model is broken in ORT 1.26.** Specifically
   `SimplifiedLayerNormFusion` references a node that doesn't exist
   after graph optimization. Use fp32 (502 MB) until upstream fix.
   Quantized int8 (127 MB) works as a middle ground if size matters.

6. **Krumhansl-Schmuckler relative-major/minor ambiguity.** C major
   and A minor share all 7 natural notes; their K-S profiles
   correlate almost identically. Confidence ("clarity") is often
   ~0.00 even when the *tonal* match is strong (raw cosine ~0.97).
   Treat key_clarity < 0.05 as "tonal class detected, major/minor
   uncertain". Not a bug, just the algorithm's known weakness.

7. **ffmpeg true peak parsing.** The `ebur128` filter labels the
   peak field as "dBFS" even when `peak=true` is set (which makes
   it True Peak per EBU R128). The numerical value IS True Peak;
   just the label is misleading.

8. **Sub-binary CGo expectations.** `yalue/onnxruntime_go` needs
   `libonnxruntime.{so,dylib,dll}` at runtime. The Go binary opens
   it via `dlopen()` at runtime, not at compile time. So the build
   doesn't need ONNX Runtime, but the deployment does. Container
   pulls `libonnxruntime` from the appropriate package.
