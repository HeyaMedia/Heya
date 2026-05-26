-- +goose Up

-- Per-user album + artist ratings — same shape as user_track_ratings.
-- Mirrors the track table so the FE can reuse the same star widget + the
-- same composable indexed by entity kind. Clears via DELETE (no zero row).

CREATE TABLE user_album_ratings (
    user_id    BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    album_id   BIGINT  NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    rating     SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 10),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, album_id)
);

CREATE INDEX user_album_ratings_user_rating_idx
    ON user_album_ratings (user_id, rating DESC, updated_at DESC);

CREATE TABLE user_artist_ratings (
    user_id    BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    artist_id  BIGINT  NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    rating     SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 10),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, artist_id)
);

CREATE INDEX user_artist_ratings_user_rating_idx
    ON user_artist_ratings (user_id, rating DESC, updated_at DESC);

-- +goose Down

DROP TABLE user_artist_ratings;
DROP TABLE user_album_ratings;
