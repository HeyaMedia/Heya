-- +goose Up

-- Internet-radio stations the user favorited. Metadata copied from the
-- radio-browser response at favorite time so the Favorites page can render
-- without re-querying the upstream API.
CREATE TABLE user_radio_favorites (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stationuuid     TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    url             TEXT        NOT NULL,
    favicon         TEXT        NOT NULL DEFAULT '',
    homepage        TEXT        NOT NULL DEFAULT '',
    country         TEXT        NOT NULL DEFAULT '',
    countrycode     TEXT        NOT NULL DEFAULT '',
    language        TEXT        NOT NULL DEFAULT '',
    tags            TEXT        NOT NULL DEFAULT '',
    codec           TEXT        NOT NULL DEFAULT '',
    bitrate         INTEGER     NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, stationuuid)
);

CREATE INDEX user_radio_favorites_user_idx ON user_radio_favorites (user_id, created_at DESC);

-- Recent stations the user has played. INSERT-only history; the FE shows
-- the latest few. Pruning policy (e.g. cap at 100 per user) lives on the
-- read query.
CREATE TABLE user_radio_recents (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    stationuuid     TEXT        NOT NULL,
    name            TEXT        NOT NULL,
    url             TEXT        NOT NULL,
    favicon         TEXT        NOT NULL DEFAULT '',
    country         TEXT        NOT NULL DEFAULT '',
    tags            TEXT        NOT NULL DEFAULT '',
    codec           TEXT        NOT NULL DEFAULT '',
    bitrate         INTEGER     NOT NULL DEFAULT 0,
    played_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX user_radio_recents_user_played_idx ON user_radio_recents (user_id, played_at DESC);

-- Podcast subscriptions. Feed URL is the canonical identifier — feeds get
-- fresh metadata on each open (cached upstream), but the subscriber list
-- needs to survive feed-metadata drift.
CREATE TABLE user_podcast_subscriptions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feed_url        TEXT        NOT NULL,
    title           TEXT        NOT NULL DEFAULT '',
    author          TEXT        NOT NULL DEFAULT '',
    artwork_url     TEXT        NOT NULL DEFAULT '',
    last_episode_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, feed_url)
);

CREATE INDEX user_podcast_subscriptions_user_idx ON user_podcast_subscriptions (user_id, created_at DESC);

-- Podcast play progress — same shape as user_watch_progress but keyed by
-- (feed_url, episode_guid) since podcast episodes don't live in media_items.
-- "Continue" surfaces resume-able episodes; the dedicated table avoids
-- polluting watch_progress with non-media-item entity types.
CREATE TABLE user_podcast_progress (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feed_url         TEXT        NOT NULL,
    episode_guid     TEXT        NOT NULL,
    title            TEXT        NOT NULL DEFAULT '',
    artwork_url      TEXT        NOT NULL DEFAULT '',
    audio_url        TEXT        NOT NULL DEFAULT '',
    progress_seconds INTEGER     NOT NULL DEFAULT 0,
    total_seconds    INTEGER     NOT NULL DEFAULT 0,
    completed        BOOLEAN     NOT NULL DEFAULT false,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, feed_url, episode_guid)
);

CREATE INDEX user_podcast_progress_user_updated_idx ON user_podcast_progress (user_id, updated_at DESC)
    WHERE completed = false AND progress_seconds > 0;

-- +goose Down
DROP TABLE IF EXISTS user_podcast_progress;
DROP TABLE IF EXISTS user_podcast_subscriptions;
DROP TABLE IF EXISTS user_radio_recents;
DROP TABLE IF EXISTS user_radio_favorites;
