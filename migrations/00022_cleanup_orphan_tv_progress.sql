-- +goose Up

-- One-shot data fix: an earlier bug recorded TV-episode playback as
-- ('movie', series_media_item_id) in user_watch_progress (the FE was
-- hardcoded to entity_type='movie' for all video, regardless of whether
-- the file was a movie or an episode). Those rows show up in the CW
-- feed as series-level entries with no episode info — wrong shape, and
-- now they shadow the correct ('episode', episode_id) rows the new
-- player produces.
--
-- Deleting them is safe: the user_watch_progress table is per-user
-- session state, not an audit log. Future playback re-creates the
-- correct rows under the right entity_type.
DELETE FROM user_watch_progress
WHERE entity_type = 'movie'
  AND entity_id IN (
    SELECT id FROM media_items WHERE media_type = 'tv'
  );

-- +goose Down

-- No-op — the bogus rows can't be reconstructed (we lost the
-- episode_id when the bug originally collapsed them onto the series).
