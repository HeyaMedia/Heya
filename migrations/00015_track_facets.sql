-- +goose Up

-- pgvector for sonic embeddings + cosine KNN.
CREATE EXTENSION IF NOT EXISTS vector;

-- Per-track sonic facets: ML embeddings + DSP measurements + waveform.
-- Populated by the sonicanalysis scheduler job (off-hours window).
CREATE TABLE track_facets (
    track_id          BIGINT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,

    -- Sonic embeddings (Discogs specialized heads + CLAP audio side)
    track_embedding   vector(512),   -- discogs_track_embeddings
    artist_embedding  vector(512),   -- discogs_artist_embeddings
    release_embedding vector(512),   -- discogs_release_embeddings
    text_embedding    vector(512),   -- CLAP audio (shares space with CLAP text)

    -- BPM
    bpm                REAL,
    bpm_confidence     REAL,

    -- Key. Root: 0=C, 1=C#, ..., 11=B. Mode: 0=major, 1=minor.
    -- Go enums (PitchClass, KeyMode) own the semantic mapping.
    key_root           SMALLINT,
    key_mode           SMALLINT,
    key_clarity        REAL,

    -- EBU R128 — duplicates the per-track values from tracks/albums
    -- loudness scan results so KNN-time we don't have to join across
    -- two unrelated migrations. Filled at analyze() time from the same
    -- ebur128 pass.
    integrated_lufs    REAL,
    loudness_range_lu  REAL,
    true_peak_dbtp     REAL,

    -- Tag outputs (kept as JSONB to avoid a wide column set per genre/mood)
    top_genres         JSONB,   -- [{"name":"Electronic---Techno","score":0.67}, ...]
    mood_tags          JSONB,   -- {"danceability":0.99,"mood_happy":0.42,...}

    -- Visualization: 2000 peak buckets in [0..1].
    waveform           REAL[],

    analyzed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    analyzer_version   INTEGER     NOT NULL DEFAULT 1
);

-- Pre-aggregated artist + album centroids. Refreshed at end of each
-- analyzer window batch (one UPSERT per affected artist/album).
-- Dedicated tables (not materialized views) so the refresh can be
-- incremental over only the artists/albums touched this window.
CREATE TABLE artist_centroids (
    artist_id        BIGINT PRIMARY KEY REFERENCES artists(id) ON DELETE CASCADE,
    sonic_centroid   vector(512),
    text_centroid    vector(512),
    track_count      INTEGER NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE album_centroids (
    album_id         BIGINT PRIMARY KEY REFERENCES albums(id) ON DELETE CASCADE,
    sonic_centroid   vector(512),
    text_centroid    vector(512),
    track_count      INTEGER NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- HNSW indexes for cosine KNN.
CREATE INDEX track_facets_track_emb_hnsw   ON track_facets    USING hnsw (track_embedding   vector_cosine_ops);
CREATE INDEX track_facets_text_emb_hnsw    ON track_facets    USING hnsw (text_embedding    vector_cosine_ops);
CREATE INDEX artist_centroids_sonic_hnsw   ON artist_centroids USING hnsw (sonic_centroid   vector_cosine_ops);
CREATE INDEX album_centroids_sonic_hnsw    ON album_centroids  USING hnsw (sonic_centroid   vector_cosine_ops);

-- "Next track to analyze" lookup: scan rows where analyzer_version is
-- behind the configured current version. Cheap btree; the query path
-- is `WHERE analyzer_version < $1` + a NOT EXISTS for unseen tracks.
CREATE INDEX track_facets_version_idx ON track_facets (analyzer_version);

-- Register the scheduled task. Defaults to a 4-hour off-hours window
-- (02:00–06:00 local) and a 240-minute hard timeout per run; the
-- scheduler's per-task DB row owns the canonical schedule going
-- forward, so users can re-tune in Settings without code changes.
INSERT INTO scheduled_tasks (
    id, display_name, description, category, enabled,
    daily_start_time, daily_end_time, max_runtime_minutes
) VALUES (
    'analyze_music_facets',
    'Analyze Music (Sonic)',
    'Per-track ML/DSP analysis: embeddings, BPM, key, loudness, mood, waveform. Runs one track at a time during the configured off-hours window.',
    'media',
    false,
    '02:00',
    '06:00',
    240
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS album_centroids;
DROP TABLE IF EXISTS artist_centroids;
DROP TABLE IF EXISTS track_facets;
