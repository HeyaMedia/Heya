-- +goose Up

CREATE TABLE user_watch_progress (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id          BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type      TEXT    NOT NULL,
    entity_id        BIGINT  NOT NULL,
    progress_seconds INT     NOT NULL DEFAULT 0,
    total_seconds    INT     NOT NULL DEFAULT 0,
    completed        BOOLEAN NOT NULL DEFAULT false,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, entity_type, entity_id)
);

CREATE INDEX idx_uwp_user ON user_watch_progress (user_id);
CREATE INDEX idx_uwp_entity ON user_watch_progress (entity_type, entity_id);
CREATE INDEX idx_uwp_continue ON user_watch_progress (user_id, completed, updated_at DESC)
    WHERE completed = false AND progress_seconds > 0;

CREATE TABLE user_favorites (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type TEXT   NOT NULL,
    entity_id   BIGINT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, entity_type, entity_id)
);

CREATE INDEX idx_user_favorites_user ON user_favorites (user_id);
CREATE INDEX idx_user_favorites_entity ON user_favorites (entity_type, entity_id);

CREATE TABLE user_lists (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT   NOT NULL,
    description TEXT   NOT NULL DEFAULT '',
    list_type   TEXT   NOT NULL DEFAULT 'manual',
    filter_json JSONB,
    media_type  TEXT   NOT NULL DEFAULT '',
    icon        TEXT   NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, name)
);

CREATE TABLE user_list_items (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    list_id       BIGINT NOT NULL REFERENCES user_lists(id) ON DELETE CASCADE,
    media_item_id BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    sort_order    INT    NOT NULL DEFAULT 0,
    added_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(list_id, media_item_id)
);

CREATE INDEX idx_user_list_items_list ON user_list_items (list_id);

CREATE TABLE user_playback_preferences (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_item_id     BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    audio_language    TEXT   NOT NULL DEFAULT '',
    subtitle_language TEXT   NOT NULL DEFAULT '',
    subtitle_mode     TEXT   NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, media_item_id)
);

CREATE INDEX idx_user_playback_prefs_user ON user_playback_preferences (user_id);

-- +goose Down
DROP TABLE user_playback_preferences;
DROP TABLE user_list_items;
DROP TABLE user_lists;
DROP TABLE user_favorites;
DROP TABLE user_watch_progress;
