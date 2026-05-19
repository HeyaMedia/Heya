-- +goose Up
CREATE TABLE watch_history (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id             BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_item_id       BIGINT      NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    progress_seconds    INTEGER     NOT NULL DEFAULT 0,
    total_seconds       INTEGER     NOT NULL DEFAULT 0,
    completed           BOOLEAN     NOT NULL DEFAULT false,
    watched_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_watch_history_user_id ON watch_history (user_id);
CREATE INDEX idx_watch_history_media_item_id ON watch_history (media_item_id);
CREATE INDEX idx_watch_history_user_media ON watch_history (user_id, media_item_id);
CREATE INDEX idx_watch_history_watched_at ON watch_history (watched_at DESC);

-- +goose Down
DROP TABLE watch_history;
