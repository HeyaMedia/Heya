-- +goose Up

-- Unified watch progress: replaces both watch_history and user_episode_watches
CREATE TABLE user_watch_progress (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type     TEXT NOT NULL,  -- 'movie' or 'episode'
    entity_id       BIGINT NOT NULL, -- media_item_id for movies, tv_episodes.id for episodes
    progress_seconds INT NOT NULL DEFAULT 0,
    total_seconds   INT NOT NULL DEFAULT 0,
    completed       BOOLEAN NOT NULL DEFAULT false,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, entity_type, entity_id)
);

CREATE INDEX idx_uwp_user ON user_watch_progress(user_id);
CREATE INDEX idx_uwp_entity ON user_watch_progress(entity_type, entity_id);
CREATE INDEX idx_uwp_continue ON user_watch_progress(user_id, completed, updated_at DESC)
    WHERE completed = false AND progress_seconds > 0;

-- Migrate existing data: episode watches become completed entries
INSERT INTO user_watch_progress (user_id, entity_type, entity_id, completed, updated_at)
SELECT user_id, 'episode', episode_id, true, watched_at
FROM user_episode_watches
ON CONFLICT DO NOTHING;

-- Migrate watch_history: movies become movie entries
INSERT INTO user_watch_progress (user_id, entity_type, entity_id, progress_seconds, total_seconds, completed, updated_at)
SELECT user_id, 'movie', media_item_id, progress_seconds, total_seconds, completed, watched_at
FROM watch_history
ON CONFLICT (user_id, entity_type, entity_id) DO UPDATE SET
    progress_seconds = EXCLUDED.progress_seconds,
    total_seconds = EXCLUDED.total_seconds,
    completed = EXCLUDED.completed,
    updated_at = EXCLUDED.updated_at;

DROP TABLE user_episode_watches;
DROP TABLE watch_history;

-- +goose Down
CREATE TABLE watch_history (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_item_id    BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    progress_seconds INT NOT NULL DEFAULT 0,
    total_seconds    INT NOT NULL DEFAULT 0,
    completed        BOOLEAN NOT NULL DEFAULT false,
    watched_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_watch_history_user_id ON watch_history(user_id);
CREATE INDEX idx_watch_history_media_item_id ON watch_history(media_item_id);
CREATE INDEX idx_watch_history_user_media ON watch_history(user_id, media_item_id);
CREATE INDEX idx_watch_history_watched_at ON watch_history(watched_at DESC);

CREATE TABLE user_episode_watches (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    episode_id BIGINT NOT NULL REFERENCES tv_episodes(id) ON DELETE CASCADE,
    watched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, episode_id)
);

DROP TABLE user_watch_progress;
