-- +goose Up

-- After the match/enrich split landed in 00017, the per-music staleness task
-- is redundant with refresh_metadata — both walked their respective tables
-- to find items past the library's MetadataRefreshDays window, and the
-- unified EnrichMediaItemWorker now handles music + non-music alike from a
-- single queue. Collapse them.
DELETE FROM scheduled_tasks WHERE id = 'refresh_music_artists';

UPDATE scheduled_tasks
   SET id           = 'refresh_stale_items',
       display_name = 'Refresh Stale Metadata',
       description  = 'Re-fetch metadata from heya.media for any media item past its library''s MetadataRefreshDays staleness window. Covers movies, TV, music, and books.'
 WHERE id = 'refresh_metadata';

-- +goose Down

UPDATE scheduled_tasks
   SET id           = 'refresh_metadata',
       display_name = 'Refresh Metadata',
       description  = 'Re-fetch metadata for stale non-music items'
 WHERE id = 'refresh_stale_items';

INSERT INTO scheduled_tasks (id, display_name, description, category, enabled)
VALUES (
    'refresh_music_artists',
    'Refresh Music Artists',
    'Re-fetch artist + album + track metadata from heya.media for music libraries, on the cadence each library configures',
    'library',
    true
)
ON CONFLICT (id) DO NOTHING;
