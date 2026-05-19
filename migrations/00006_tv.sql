-- +goose Up
CREATE TABLE tv_series (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT  NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    tmdb_id         INTEGER,
    imdb_id         TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT '',
    genres          TEXT[]      NOT NULL DEFAULT '{}',
    rating          NUMERIC(3,1) NOT NULL DEFAULT 0,
    first_air_date  DATE,
    last_air_date   DATE
);

CREATE INDEX idx_tv_series_tmdb_id ON tv_series (tmdb_id);

CREATE TABLE tv_seasons (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id       BIGINT      NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    season_number   INTEGER     NOT NULL,
    title           TEXT        NOT NULL DEFAULT '',
    overview        TEXT        NOT NULL DEFAULT '',
    poster_path     TEXT        NOT NULL DEFAULT '',
    air_date        DATE,
    UNIQUE (series_id, season_number)
);

CREATE TABLE tv_episodes (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    season_id       BIGINT      NOT NULL REFERENCES tv_seasons(id) ON DELETE CASCADE,
    episode_number  INTEGER     NOT NULL,
    title           TEXT        NOT NULL DEFAULT '',
    overview        TEXT        NOT NULL DEFAULT '',
    still_path      TEXT        NOT NULL DEFAULT '',
    runtime_minutes INTEGER     NOT NULL DEFAULT 0,
    air_date        DATE,
    UNIQUE (season_id, episode_number)
);

-- +goose Down
DROP TABLE tv_episodes;
DROP TABLE tv_seasons;
DROP TABLE tv_series;
