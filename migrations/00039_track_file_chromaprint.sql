-- +goose Up

-- Chromaprint audio fingerprints, per FILE (not per track): different encodes
-- of the same recording produce near-identical but not bit-identical
-- fingerprints, and per-file provenance is what a future heya.media
-- submission API needs. Stored as chromaprint's base64 compressed form (the
-- AcoustID interchange format, URL-safe alphabet, no padding). The fingerprint
-- covers at most the first ~120s of audio (fpcalc's default window), which is
-- plenty for same-recording detection but means a radio edit can match a full
-- version's prefix — duration stays a second gate in any dedupe pass.
--
-- chromaprint_algorithm records the fingerprint algorithm version (chromaprint
-- TEST2 = 1, the fpcalc/ffmpeg default) so a future algorithm bump knows which
-- rows to regenerate. chromaprint_duration_secs is the seconds of audio the
-- fingerprint actually covers. fingerprinted_at NULL = not yet analyzed (the
-- pump's pending filter, same convention as loudness_analyzed_at).
ALTER TABLE track_files
    ADD COLUMN chromaprint TEXT,
    ADD COLUMN chromaprint_algorithm SMALLINT,
    ADD COLUMN chromaprint_duration_secs INTEGER,
    ADD COLUMN fingerprinted_at TIMESTAMPTZ;

-- Scheduled pump for the fingerprint pass — same loudness-style snooze-loop
-- kickoff, sweeping track_files missing a fingerprint. Cheap per file (~1-2s:
-- decodes only the first 120s), CPU-bound, no GPU, no external calls.
INSERT INTO scheduled_tasks (id, display_name, description, category, enabled)
VALUES (
    'scan_music_fingerprint',
    'Scan Music Fingerprints',
    'Chromaprint audio fingerprints for music files — powers duplicate-recording detection and future fingerprint submission',
    'library',
    true
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM scheduled_tasks WHERE id = 'scan_music_fingerprint';
ALTER TABLE track_files
    DROP COLUMN IF EXISTS chromaprint,
    DROP COLUMN IF EXISTS chromaprint_algorithm,
    DROP COLUMN IF EXISTS chromaprint_duration_secs,
    DROP COLUMN IF EXISTS fingerprinted_at;
