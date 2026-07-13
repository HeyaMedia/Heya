-- +goose Up

-- Converge the two file-link generations: track_files is the current
-- model (scanner writes it; track detail / quality picker / loudness /
-- cast all key on it), but tracks scanned before it existed only carry
-- the legacy tracks.library_file_id link — leaving them uncastable and
-- never loudness-analyzed. Synthesize the missing rows from the legacy
-- link: format from the file extension, duration from the track row,
-- size from the library file. quality_score stays 0 (single file — it
-- only breaks ties) and probe data fills in on first playback analysis.
INSERT INTO track_files (track_id, library_file_id, format, duration, size_bytes)
SELECT t.id,
       t.library_file_id,
       CASE WHEN lf.path ~ '\.[A-Za-z0-9]+$'
            THEN lower(substring(lf.path from '\.([A-Za-z0-9]+)$'))
            ELSE '' END,
       t.duration,
       lf.size
FROM tracks t
JOIN library_files lf ON lf.id = t.library_file_id AND lf.deleted_at IS NULL
WHERE NOT EXISTS (SELECT 1 FROM track_files tf WHERE tf.track_id = t.id)
ON CONFLICT (library_file_id) DO NOTHING;

-- +goose Down

-- Data backfill — no clean way to tell synthesized rows apart later, and
-- removing them would re-break the tracks. Intentional no-op.
SELECT 1;
