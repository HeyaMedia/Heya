-- +goose Up

-- Favorites: polymorphic likes on any entity
CREATE TABLE user_favorites (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_type     TEXT NOT NULL,
    entity_id       BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, entity_type, entity_id)
);
CREATE INDEX idx_user_favorites_user ON user_favorites(user_id);
CREATE INDEX idx_user_favorites_entity ON user_favorites(entity_type, entity_id);

-- User lists
CREATE TABLE user_lists (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, name)
);

CREATE TABLE user_list_items (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    list_id         BIGINT NOT NULL REFERENCES user_lists(id) ON DELETE CASCADE,
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    sort_order      INT NOT NULL DEFAULT 0,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(list_id, media_item_id)
);
CREATE INDEX idx_user_list_items_list ON user_list_items(list_id);

-- Episode-level watched tracking
CREATE TABLE user_episode_watches (
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    episode_id      BIGINT NOT NULL REFERENCES tv_episodes(id) ON DELETE CASCADE,
    watched_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, episode_id)
);

-- +goose Down
DROP TABLE IF EXISTS user_episode_watches;
DROP TABLE IF EXISTS user_list_items;
DROP TABLE IF EXISTS user_lists;
DROP TABLE IF EXISTS user_favorites;
