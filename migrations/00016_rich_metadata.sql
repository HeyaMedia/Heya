-- +goose Up

-- People (actors, directors, writers, etc.) — shared across all media
CREATE TABLE people (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tmdb_id         INTEGER UNIQUE,
    name            TEXT NOT NULL DEFAULT '',
    also_known_as   TEXT[] NOT NULL DEFAULT '{}',
    biography       TEXT NOT NULL DEFAULT '',
    birthday        TEXT NOT NULL DEFAULT '',
    deathday        TEXT NOT NULL DEFAULT '',
    place_of_birth  TEXT NOT NULL DEFAULT '',
    gender          INTEGER NOT NULL DEFAULT 0,
    profile_path    TEXT NOT NULL DEFAULT '',
    homepage        TEXT NOT NULL DEFAULT '',
    imdb_id         TEXT NOT NULL DEFAULT '',
    popularity      NUMERIC(10,3) NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_people_tmdb_id ON people(tmdb_id);
CREATE INDEX idx_people_name ON people(name);

-- Cast credits linking people to media items
CREATE TABLE media_cast (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    person_id       BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    character       TEXT NOT NULL DEFAULT '',
    display_order   INTEGER NOT NULL DEFAULT 0,
    UNIQUE(media_item_id, person_id, character)
);
CREATE INDEX idx_media_cast_media ON media_cast(media_item_id);
CREATE INDEX idx_media_cast_person ON media_cast(person_id);

-- Crew credits linking people to media items
CREATE TABLE media_crew (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    person_id       BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    job             TEXT NOT NULL DEFAULT '',
    department      TEXT NOT NULL DEFAULT '',
    UNIQUE(media_item_id, person_id, job)
);
CREATE INDEX idx_media_crew_media ON media_crew(media_item_id);
CREATE INDEX idx_media_crew_person ON media_crew(person_id);

-- Keywords / tags
CREATE TABLE keywords (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tmdb_id         INTEGER UNIQUE,
    name            TEXT NOT NULL DEFAULT ''
);

CREATE TABLE media_keywords (
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    keyword_id      BIGINT NOT NULL REFERENCES keywords(id) ON DELETE CASCADE,
    PRIMARY KEY (media_item_id, keyword_id)
);

-- Production companies as proper entities
CREATE TABLE production_companies (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tmdb_id         INTEGER UNIQUE,
    name            TEXT NOT NULL DEFAULT '',
    logo_path       TEXT NOT NULL DEFAULT '',
    origin_country  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE media_production_companies (
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    company_id      BIGINT NOT NULL REFERENCES production_companies(id) ON DELETE CASCADE,
    PRIMARY KEY (media_item_id, company_id)
);

-- Certifications / release dates by country
CREATE TABLE media_certifications (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    country         TEXT NOT NULL DEFAULT '',
    certification   TEXT NOT NULL DEFAULT '',
    release_date    DATE,
    release_type    INTEGER NOT NULL DEFAULT 0,
    UNIQUE(media_item_id, country, release_type)
);
CREATE INDEX idx_media_cert_media ON media_certifications(media_item_id);

-- Videos (trailers, featurettes from TMDB — YouTube links)
CREATE TABLE media_videos (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id   BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    tmdb_key        TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    site            TEXT NOT NULL DEFAULT '',
    video_key       TEXT NOT NULL DEFAULT '',
    video_type      TEXT NOT NULL DEFAULT '',
    language        TEXT NOT NULL DEFAULT '',
    official        BOOLEAN NOT NULL DEFAULT false,
    published_at    TIMESTAMPTZ,
    UNIQUE(media_item_id, video_key)
);
CREATE INDEX idx_media_videos_media ON media_videos(media_item_id);

-- Recommendations (links between media items)
CREATE TABLE media_recommendations (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id       BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    recommended_tmdb_id INTEGER NOT NULL,
    title               TEXT NOT NULL DEFAULT '',
    poster_path         TEXT NOT NULL DEFAULT '',
    media_type          TEXT NOT NULL DEFAULT '',
    vote_average        NUMERIC(3,1) NOT NULL DEFAULT 0,
    release_date        TEXT NOT NULL DEFAULT '',
    UNIQUE(media_item_id, recommended_tmdb_id)
);
CREATE INDEX idx_media_rec_media ON media_recommendations(media_item_id);

-- Collections (e.g., "Alien Collection")
CREATE TABLE collections (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tmdb_id         INTEGER UNIQUE,
    name            TEXT NOT NULL DEFAULT '',
    overview        TEXT NOT NULL DEFAULT '',
    poster_path     TEXT NOT NULL DEFAULT '',
    backdrop_path   TEXT NOT NULL DEFAULT ''
);

-- Add collection reference to movies
ALTER TABLE movies ADD COLUMN collection_id BIGINT REFERENCES collections(id);
ALTER TABLE movies ADD COLUMN status TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN homepage TEXT NOT NULL DEFAULT '';
ALTER TABLE movies ADD COLUMN spoken_languages TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE movies ADD COLUMN origin_country TEXT[] NOT NULL DEFAULT '{}';

-- Add extra fields to media_items
ALTER TABLE media_items ADD COLUMN homepage TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN wikidata_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN facebook_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN instagram_id TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN twitter_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE media_items DROP COLUMN IF EXISTS twitter_id;
ALTER TABLE media_items DROP COLUMN IF EXISTS instagram_id;
ALTER TABLE media_items DROP COLUMN IF EXISTS facebook_id;
ALTER TABLE media_items DROP COLUMN IF EXISTS wikidata_id;
ALTER TABLE media_items DROP COLUMN IF EXISTS homepage;
ALTER TABLE movies DROP COLUMN IF EXISTS origin_country;
ALTER TABLE movies DROP COLUMN IF EXISTS spoken_languages;
ALTER TABLE movies DROP COLUMN IF EXISTS homepage;
ALTER TABLE movies DROP COLUMN IF EXISTS status;
ALTER TABLE movies DROP COLUMN IF EXISTS collection_id;
DROP TABLE IF EXISTS collections;
DROP TABLE IF EXISTS media_recommendations;
DROP TABLE IF EXISTS media_videos;
DROP TABLE IF EXISTS media_certifications;
DROP TABLE IF EXISTS media_production_companies;
DROP TABLE IF EXISTS production_companies;
DROP TABLE IF EXISTS media_keywords;
DROP TABLE IF EXISTS keywords;
DROP TABLE IF EXISTS media_crew;
DROP TABLE IF EXISTS media_cast;
DROP TABLE IF EXISTS people;
