-- +goose Up

-- The track_files / albums loudness pipeline is the canonical source of
-- EBU R128 numbers. It runs as soon as a music file is probed (not gated
-- on sonic analysis being enabled), feeds the audio engine's normalization
-- block, and computes proper album-mode loudness via ffmpeg's concat
-- demuxer. The duplicate columns under track_facets were a redundant
-- per-track measurement that wasted ~1-2s of ffmpeg per track per analyze
-- run. Removing them cuts the sonic-analysis pass time without losing any
-- user-visible feature — the read path (FacetsView, UI chips) now joins
-- track_files for loudness instead.

ALTER TABLE track_facets DROP COLUMN IF EXISTS integrated_lufs;
ALTER TABLE track_facets DROP COLUMN IF EXISTS loudness_range_lu;
ALTER TABLE track_facets DROP COLUMN IF EXISTS true_peak_dbtp;

-- +goose Down

-- Down restores the columns as nullable so the original migration's
-- analyzer pipeline can be re-enabled if someone reverts this.
ALTER TABLE track_facets ADD COLUMN integrated_lufs   REAL;
ALTER TABLE track_facets ADD COLUMN loudness_range_lu REAL;
ALTER TABLE track_facets ADD COLUMN true_peak_dbtp    REAL;
