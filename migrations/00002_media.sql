-- +goose Up

CREATE TABLE media_items (
    id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_id            BIGINT      NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    media_type            media_type  NOT NULL,
    title                 TEXT        NOT NULL,
    sort_title            TEXT        NOT NULL DEFAULT '',
    year                  TEXT        NOT NULL DEFAULT '',
    description           TEXT        NOT NULL DEFAULT '',
    poster_path           TEXT        NOT NULL DEFAULT '',
    backdrop_path         TEXT        NOT NULL DEFAULT '',
    external_ids          JSONB       NOT NULL DEFAULT '{}',
    slug                  TEXT        NOT NULL DEFAULT '',
    homepage              TEXT        NOT NULL DEFAULT '',
    tagline               TEXT        NOT NULL DEFAULT '',
    original_title        TEXT        NOT NULL DEFAULT '',
    original_language     TEXT        NOT NULL DEFAULT '',
    status                TEXT        NOT NULL DEFAULT '',
    provider_kind         TEXT        NOT NULL DEFAULT '',
    heya_slug             TEXT        NOT NULL DEFAULT '',
    heya_enriched_at      TIMESTAMPTZ,
    metadata_refreshed_at TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    search_vector         tsvector GENERATED ALWAYS AS (
                              to_tsvector('english', title || ' ' || coalesce(description, ''))
                          ) STORED
);

CREATE INDEX idx_media_items_library_id ON media_items (library_id);
CREATE INDEX idx_media_items_media_type ON media_items (media_type);
CREATE INDEX idx_media_items_title ON media_items (title);
CREATE INDEX idx_media_items_search ON media_items USING GIN (search_vector);
CREATE UNIQUE INDEX idx_media_items_slug ON media_items (slug) WHERE slug != '';
CREATE INDEX idx_media_items_title_trgm ON media_items USING GIN (lower(title) gin_trgm_ops);
CREATE INDEX idx_media_items_external_ids ON media_items USING GIN (external_ids);
CREATE UNIQUE INDEX idx_media_items_tmdb_unique ON media_items (library_id, (external_ids->>'tmdb')) WHERE external_ids->>'tmdb' IS NOT NULL;
CREATE UNIQUE INDEX idx_media_items_tvdb_unique ON media_items (library_id, (external_ids->>'tvdb')) WHERE external_ids->>'tvdb' IS NOT NULL;
CREATE UNIQUE INDEX idx_media_items_imdb_unique ON media_items (library_id, (external_ids->>'imdb')) WHERE external_ids->>'imdb' IS NOT NULL;
CREATE UNIQUE INDEX idx_media_items_mbid_unique ON media_items (library_id, (external_ids->>'mbid')) WHERE external_ids->>'mbid' IS NOT NULL;
CREATE UNIQUE INDEX idx_media_items_ol_work_id_unique ON media_items (library_id, (external_ids->>'ol_work_id')) WHERE external_ids->>'ol_work_id' IS NOT NULL;
CREATE UNIQUE INDEX idx_media_items_heya_slug ON media_items (heya_slug) WHERE heya_slug != '';

CREATE TABLE collections (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_ids  JSONB NOT NULL DEFAULT '{}',
    name          TEXT NOT NULL DEFAULT '',
    overview      TEXT NOT NULL DEFAULT '',
    poster_path   TEXT NOT NULL DEFAULT '',
    backdrop_path TEXT NOT NULL DEFAULT '',
    search_vector tsvector GENERATED ALWAYS AS (
                      setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
                      setweight(to_tsvector('english', coalesce(overview, '')), 'D')
                  ) STORED
);

CREATE INDEX idx_collections_search ON collections USING GIN (search_vector);
CREATE INDEX idx_collections_name_trgm ON collections USING GIN (lower(name) gin_trgm_ops);

CREATE TABLE movies (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id     BIGINT       NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    runtime_minutes   INTEGER      NOT NULL DEFAULT 0,
    tagline           TEXT         NOT NULL DEFAULT '',
    genres            TEXT[]       NOT NULL DEFAULT '{}',
    rating            NUMERIC(5,2) NOT NULL DEFAULT 0,
    release_date      DATE,
    original_title    TEXT         NOT NULL DEFAULT '',
    original_language TEXT         NOT NULL DEFAULT '',
    budget            BIGINT       NOT NULL DEFAULT 0,
    revenue           BIGINT       NOT NULL DEFAULT 0,
    popularity        NUMERIC(10,3) NOT NULL DEFAULT 0,
    collection_id     BIGINT       REFERENCES collections(id),
    status            TEXT         NOT NULL DEFAULT '',
    homepage          TEXT         NOT NULL DEFAULT '',
    spoken_languages  TEXT[]       NOT NULL DEFAULT '{}',
    origin_country    TEXT[]       NOT NULL DEFAULT '{}'
);

CREATE TABLE tv_series (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id      BIGINT       NOT NULL UNIQUE REFERENCES media_items(id) ON DELETE CASCADE,
    status             TEXT         NOT NULL DEFAULT '',
    genres             TEXT[]       NOT NULL DEFAULT '{}',
    rating             NUMERIC(5,2) NOT NULL DEFAULT 0,
    first_air_date     DATE,
    last_air_date      DATE,
    original_name      TEXT         NOT NULL DEFAULT '',
    original_language  TEXT         NOT NULL DEFAULT '',
    number_of_seasons  INTEGER      NOT NULL DEFAULT 0,
    number_of_episodes INTEGER      NOT NULL DEFAULT 0,
    popularity         NUMERIC(10,3) NOT NULL DEFAULT 0,
    spoken_languages   TEXT[]       NOT NULL DEFAULT '{}',
    origin_country     TEXT[]       NOT NULL DEFAULT '{}'
);

CREATE TABLE tv_seasons (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    series_id      BIGINT  NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    season_number  INTEGER NOT NULL,
    title          TEXT    NOT NULL DEFAULT '',
    overview       TEXT    NOT NULL DEFAULT '',
    poster_path    TEXT    NOT NULL DEFAULT '',
    air_date       DATE,
    end_date       DATE,
    status         TEXT    NOT NULL DEFAULT '',
    aired_episodes INTEGER NOT NULL DEFAULT 0,
    external_ids   JSONB   NOT NULL DEFAULT '{}',
    UNIQUE (series_id, season_number)
);

CREATE TABLE tv_episodes (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    season_id       BIGINT       NOT NULL REFERENCES tv_seasons(id) ON DELETE CASCADE,
    episode_number  INTEGER      NOT NULL,
    title           TEXT         NOT NULL DEFAULT '',
    overview        TEXT         NOT NULL DEFAULT '',
    still_path      TEXT         NOT NULL DEFAULT '',
    runtime_minutes INTEGER      NOT NULL DEFAULT 0,
    air_date        DATE,
    rating          NUMERIC(5,2) NOT NULL DEFAULT 0,
    absolute_number INTEGER      NOT NULL DEFAULT 0,
    is_special      BOOLEAN      NOT NULL DEFAULT false,
    episode_type    INTEGER      NOT NULL DEFAULT 1,
    external_ids    JSONB        NOT NULL DEFAULT '{}',
    source          TEXT         NOT NULL DEFAULT '',
    UNIQUE (season_id, episode_number)
);

CREATE TABLE episode_titles (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    episode_id BIGINT NOT NULL REFERENCES tv_episodes(id) ON DELETE CASCADE,
    title      TEXT   NOT NULL,
    language   TEXT   NOT NULL DEFAULT '',
    source     TEXT   NOT NULL DEFAULT '',
    UNIQUE(episode_id, language)
);

CREATE INDEX idx_episode_titles_episode ON episode_titles (episode_id);

CREATE TABLE episode_overviews (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    episode_id BIGINT NOT NULL REFERENCES tv_episodes(id) ON DELETE CASCADE,
    language   TEXT   NOT NULL,
    overview   TEXT   NOT NULL DEFAULT '',
    UNIQUE(episode_id, language)
);

CREATE INDEX idx_episode_overviews_episode ON episode_overviews (episode_id);

CREATE TABLE media_titles (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    title         TEXT   NOT NULL,
    language      TEXT   NOT NULL DEFAULT '',
    country       TEXT   NOT NULL DEFAULT '',
    title_type    TEXT   NOT NULL DEFAULT 'translation',
    source        TEXT   NOT NULL DEFAULT '',
    UNIQUE(media_item_id, title, language)
);

CREATE INDEX idx_media_titles_media ON media_titles (media_item_id);

CREATE TABLE media_overviews (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    language      TEXT   NOT NULL,
    overview      TEXT   NOT NULL DEFAULT '',
    UNIQUE(media_item_id, language)
);

CREATE INDEX idx_media_overviews_media ON media_overviews (media_item_id);

-- +goose Down
DROP TABLE media_overviews;
DROP TABLE media_titles;
DROP TABLE episode_overviews;
DROP TABLE episode_titles;
DROP TABLE tv_episodes;
DROP TABLE tv_seasons;
DROP TABLE tv_series;
DROP TABLE movies;
DROP TABLE collections;
DROP TABLE media_items;
