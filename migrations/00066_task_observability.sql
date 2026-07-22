-- +goose Up

ALTER TABLE public.scheduled_tasks
    ADD COLUMN IF NOT EXISTS last_run_error text NOT NULL DEFAULT '';

UPDATE public.scheduled_tasks
SET category = CASE
    WHEN id IN (
        'scan_music_loudness',
        'scan_music_fingerprint',
        'analyze_music_facets',
        'sync_music_services'
    ) THEN 'audio'
    WHEN id IN (
        'generate_trickplay',
        'generate_thumbnails',
        'scan_media_segments',
        'detect_media_segments',
        'embed_recommendations'
    ) THEN 'video'
    ELSE 'general'
END;

-- +goose Down

UPDATE public.scheduled_tasks
SET category = CASE
    WHEN id IN ('generate_trickplay', 'generate_thumbnails', 'analyze_music_facets') THEN 'media'
    WHEN id = 'cleanup_scanner_artifacts' THEN 'maintenance'
    ELSE 'library'
END;

ALTER TABLE public.scheduled_tasks
    DROP COLUMN IF EXISTS last_run_error;
