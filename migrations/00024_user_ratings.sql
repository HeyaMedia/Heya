-- +goose Up

-- Per-user track ratings. Stored 1..10 internally to allow half-star UI
-- on a 5-star display (1=½★, 2=★, ..., 10=★★★★★). Single rating per
-- (user, track) — upsert on PK collision.
--
-- "favorite" is derived: any track with rating >= favorites_threshold (a
-- per-user preference; default 7 = 3.5★) is treated as a favorite by the
-- Favorites view. Keep the threshold dynamic so we can later swap the
-- 5-star UI for a 10-point UI without rewriting the data model.
CREATE TABLE user_track_ratings (
    user_id    BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id   BIGINT  NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    rating     SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 10),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, track_id)
);

-- Drives the Favorites view (everything rated above threshold). The
-- partial index keeps it small — most tracks won't have ratings.
CREATE INDEX user_track_ratings_user_rating_idx
    ON user_track_ratings (user_id, rating DESC, updated_at DESC);

-- Per-user preference: where the "favorite" threshold sits on the 1..10
-- scale. Default 7 = 3.5 stars in the 5-star UI, the natural "I really
-- like this" bar without demanding a perfect score. Configurable later
-- via Settings → My Music → Favorites threshold.
ALTER TABLE users
    ADD COLUMN favorites_threshold SMALLINT NOT NULL DEFAULT 7
        CHECK (favorites_threshold BETWEEN 1 AND 10);

-- +goose Down

ALTER TABLE users DROP COLUMN favorites_threshold;
DROP TABLE user_track_ratings;
