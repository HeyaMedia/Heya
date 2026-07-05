-- +goose Up

-- Local Intro-Skipper-style detection: cross-episode chromaprint matching
-- for intro/credits on TV seasons, and ffmpeg black-frame detection for
-- movie credits — run for files the community pump (scan_media_segments)
-- already checked (segments_analyzed_at set) but couldn't resolve. NULL =
-- local detection not yet attempted, mirroring segments_analyzed_at's
-- pending-sentinel convention.
ALTER TABLE library_files ADD COLUMN segments_detected_at TIMESTAMPTZ;

INSERT INTO scheduled_tasks (id, display_name, description, category, enabled)
VALUES (
    'detect_media_segments',
    'Detect Skip Segments',
    'Local chromaprint cross-episode intro/credits detection for TV and ffmpeg black-frame credits detection for movies, for files the community skip-segment databases could not resolve',
    'library',
    true
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM scheduled_tasks WHERE id = 'detect_media_segments';
ALTER TABLE library_files DROP COLUMN segments_detected_at;
