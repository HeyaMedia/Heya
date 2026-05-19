-- +goose Up
CREATE TABLE movies (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT  NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    tmdb_id         INTEGER,
    imdb_id         TEXT        NOT NULL DEFAULT '',
    runtime_minutes INTEGER     NOT NULL DEFAULT 0,
    tagline         TEXT        NOT NULL DEFAULT '',
    genres          TEXT[]      NOT NULL DEFAULT '{}',
    rating          NUMERIC(3,1) NOT NULL DEFAULT 0,
    release_date    DATE
);

CREATE INDEX idx_movies_tmdb_id ON movies (tmdb_id);
CREATE INDEX idx_movies_imdb_id ON movies (imdb_id) WHERE imdb_id != '';

-- +goose Down
DROP TABLE movies;
