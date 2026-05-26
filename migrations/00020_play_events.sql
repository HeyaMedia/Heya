-- +goose Up

-- play_events records "I listened to this" scrobbles, one row per qualifying
-- play (≥30s heard, or track-end). Feeds:
--   - /api/me/recently-played (the "Resume / play next" rail)
--   - /api/me/listening-stats (per-user top-genres + mood profile + tempo
--     histogram, derived by joining track_facets at query time)
--
-- We keep this minimal: no per-event mood/genre snapshot — those are joined
-- at read time from track_facets so the schema stays small and the analyzer
-- can rewrite facets without invalidating history.
CREATE TABLE play_events (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT      NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    track_id          BIGINT      NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    listened_seconds  INTEGER     NOT NULL,
    completed         BOOLEAN     NOT NULL DEFAULT false,
    -- Origin label for analytics — 'queue' | 'radio' | 'album' | 'playlist'
    -- | 'search' | 'browse' | 'similar' | ''. Free-form for future surfaces;
    -- the FE owns the vocabulary, the DB doesn't gate it.
    source            TEXT        NOT NULL DEFAULT ''
);

-- The recently-played list paginates user_id-scoped events newest-first;
-- this composite covers both the filter and the order so PG can answer
-- straight from the index.
CREATE INDEX play_events_user_played_idx ON play_events (user_id, played_at DESC);

-- Per-track lookups (e.g. "how many times have I played this track") use
-- the track-id leg.
CREATE INDEX play_events_track_idx ON play_events (track_id);

-- +goose Down
DROP TABLE IF EXISTS play_events;
