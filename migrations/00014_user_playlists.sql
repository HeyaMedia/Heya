-- +goose Up

CREATE TABLE user_playlists (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT   NOT NULL,
    description TEXT   NOT NULL DEFAULT '',
    -- cover_path stays empty when the user doesn't pick one; the UI then
    -- synthesizes a 2x2 mosaic from the first 4 track covers at render time.
    cover_path  TEXT   NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_user_playlists_user ON user_playlists (user_id);

CREATE TABLE user_playlist_tracks (
    playlist_id BIGINT NOT NULL REFERENCES user_playlists(id) ON DELETE CASCADE,
    track_id    BIGINT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (playlist_id, track_id)
);

CREATE INDEX idx_user_playlist_tracks_playlist_position ON user_playlist_tracks (playlist_id, position);

-- +goose Down
DROP TABLE user_playlist_tracks;
DROP TABLE user_playlists;
