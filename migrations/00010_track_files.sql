-- +goose Up

-- track_files holds N rows per tracks row, one per physical audio file.
-- tracks.{file_path, library_file_id, duration, lyrics_path} stay as
-- denormalized "primary" pointers (the highest-quality file we'd auto-play),
-- so the existing playback URL path doesn't need an extra join.
--
-- Phase 6a fills only format + quality_score (extension-based). Phase 6b
-- adds an ffprobe worker that populates bitrate_kbps / sample_rate_hz /
-- bit_depth / channels / duration / size_bytes, refining quality_score.
CREATE TABLE track_files (
    id              BIGINT  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    track_id        BIGINT  NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    library_file_id BIGINT  NOT NULL UNIQUE REFERENCES library_files(id) ON DELETE CASCADE,
    format          TEXT    NOT NULL DEFAULT '',
    quality_score   INTEGER NOT NULL DEFAULT 0,
    bitrate_kbps    INTEGER NOT NULL DEFAULT 0,
    sample_rate_hz  INTEGER NOT NULL DEFAULT 0,
    bit_depth       INTEGER NOT NULL DEFAULT 0,
    channels        INTEGER NOT NULL DEFAULT 0,
    duration        INTEGER NOT NULL DEFAULT 0,
    size_bytes      BIGINT  NOT NULL DEFAULT 0,
    lyrics_path     TEXT    NOT NULL DEFAULT '',
    -- EBU R128 loudness fields populated by ScanTrackLoudnessWorker. NULL
    -- means "not yet analyzed". integrated_lufs and true_peak_db are what
    -- the engine's normalization block consumes; loudness_range_db and
    -- sample_peak_db are kept for the track-info popover.
    integrated_lufs     NUMERIC(6, 2),
    true_peak_db        NUMERIC(6, 2),
    loudness_range_db   NUMERIC(6, 2),
    sample_peak_db      NUMERIC(6, 2),
    loudness_analyzed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_track_files_track ON track_files (track_id);
CREATE INDEX idx_track_files_quality ON track_files (track_id, quality_score DESC);

-- +goose Down
DROP TABLE track_files;
