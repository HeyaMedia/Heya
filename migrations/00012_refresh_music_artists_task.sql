-- +goose Up

-- Periodic task that finds artists whose enriched_at is older than the
-- library's MetadataRefreshDays setting and enqueues a RefreshMusicArtist job
-- for each. Runs serialised on the music_metadata queue to avoid hammering
-- heya.media with cold lookups.
INSERT INTO scheduled_tasks (id, display_name, description, category, enabled)
VALUES (
    'refresh_music_artists',
    'Refresh Music Artists',
    'Re-fetch artist + album + track metadata from heya.media for music libraries, on the cadence each library configures',
    'library',
    true
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM scheduled_tasks WHERE id = 'refresh_music_artists';
