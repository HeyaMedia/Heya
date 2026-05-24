-- +goose Up

-- Periodic backstop for the loudness pipeline. Catches files probed before
-- the loudness columns existed, and files whose matcher-created track_files
-- row appeared after the ffprobe hand-off (so the inline enqueue saw no
-- row to attach to). Also enqueues album-level analysis for albums whose
-- tracks have all completed but never got the album pass (cascade crash).
INSERT INTO scheduled_tasks (id, display_name, description, category, enabled)
VALUES (
    'scan_music_loudness',
    'Scan Music Loudness',
    'Backstop for the ebur128 pipeline — enqueues track and album loudness analysis for any music files not yet measured',
    'library',
    true
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM scheduled_tasks WHERE id = 'scan_music_loudness';
