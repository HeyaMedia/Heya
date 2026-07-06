-- +goose Up
-- Repair mis-attributed TV watch progress.
--
-- Episode playback paths that didn't pass entity_type=episode recorded
-- progress as ('movie', series_media_item_id) instead of
-- ('episode', episode_id). Symptoms: TV shows never left Continue Watching
-- (mark-episode-watched keys on ('episode', id) and so never touched the
-- 'movie' row), and completion was shared across a whole series rather than
-- per episode.
--
-- The frontend now always keys episode playback on the episode. There is no
-- reliable way to map a series-level row back to the episode it came from
-- (the row carries no episode identity), so drop the corrupt rows — the user
-- re-appears in Continue Watching correctly the next time they play.
DELETE FROM user_watch_progress
WHERE entity_type = 'movie'
  AND entity_id IN (SELECT id FROM media_items WHERE media_type = 'tv');

-- +goose Down
-- One-way data repair — the discarded rows held no episode identity to restore.
SELECT 1;
