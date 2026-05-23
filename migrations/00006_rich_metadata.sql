-- +goose Up

CREATE TABLE people (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_ids         JSONB        NOT NULL DEFAULT '{}',
    name                 TEXT         NOT NULL DEFAULT '',
    also_known_as        TEXT[]       NOT NULL DEFAULT '{}',
    biography            TEXT         NOT NULL DEFAULT '',
    birthday             TEXT         NOT NULL DEFAULT '',
    deathday             TEXT         NOT NULL DEFAULT '',
    place_of_birth       TEXT         NOT NULL DEFAULT '',
    gender               INTEGER      NOT NULL DEFAULT 0,
    profile_path         TEXT         NOT NULL DEFAULT '',
    homepage             TEXT         NOT NULL DEFAULT '',
    popularity           NUMERIC(10,3) NOT NULL DEFAULT 0,
    slug                 TEXT         NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    sort_name            TEXT         NOT NULL DEFAULT '',
    known_for_department TEXT         NOT NULL DEFAULT '',
    birth_year           INTEGER      NOT NULL DEFAULT 0,
    heya_slug            TEXT         NOT NULL DEFAULT '',
    heya_enriched_at     TIMESTAMPTZ,
    search_vector        tsvector GENERATED ALWAYS AS (
                             setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
                             setweight(to_tsvector('simple', immutable_array_to_string(coalesce(also_known_as, '{}'::text[]), ' ')), 'B') ||
                             setweight(to_tsvector('english', coalesce(biography, '')), 'D')
                         ) STORED
);

CREATE INDEX idx_people_name ON people (name);
CREATE UNIQUE INDEX idx_people_slug ON people (slug) WHERE slug != '';
CREATE INDEX idx_people_search ON people USING GIN (search_vector);
CREATE INDEX idx_people_name_trgm ON people USING GIN (lower(name) gin_trgm_ops);
CREATE UNIQUE INDEX idx_people_heya_slug ON people (heya_slug) WHERE heya_slug != '';
CREATE INDEX idx_people_external_ids ON people USING GIN (external_ids);

CREATE TABLE media_cast (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT  NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    person_id     BIGINT  NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    character     TEXT    NOT NULL DEFAULT '',
    display_order INTEGER NOT NULL DEFAULT 0,
    gender        INTEGER NOT NULL DEFAULT 0,
    source        TEXT    NOT NULL DEFAULT '',
    UNIQUE(media_item_id, person_id, character)
);

CREATE INDEX idx_media_cast_media ON media_cast (media_item_id);
CREATE INDEX idx_media_cast_person ON media_cast (person_id);

CREATE TABLE media_crew (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT  NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    person_id     BIGINT  NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    job           TEXT    NOT NULL DEFAULT '',
    department    TEXT    NOT NULL DEFAULT '',
    gender        INTEGER NOT NULL DEFAULT 0,
    source        TEXT    NOT NULL DEFAULT '',
    UNIQUE(media_item_id, person_id, job)
);

CREATE INDEX idx_media_crew_media ON media_crew (media_item_id);
CREATE INDEX idx_media_crew_person ON media_crew (person_id);

CREATE TABLE networks (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         TEXT  NOT NULL,
    external_ids JSONB NOT NULL DEFAULT '{}',
    logo_path    TEXT  NOT NULL DEFAULT '',
    country      TEXT  NOT NULL DEFAULT '',
    UNIQUE (name)
);
CREATE INDEX idx_networks_external_ids ON networks USING GIN (external_ids);

CREATE TABLE tv_series_networks (
    series_id  BIGINT  NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    network_id BIGINT  NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (series_id, network_id)
);

CREATE TABLE creators (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         TEXT  NOT NULL,
    external_ids JSONB NOT NULL DEFAULT '{}',
    UNIQUE (name)
);
CREATE INDEX idx_creators_external_ids ON creators USING GIN (external_ids);

CREATE TABLE tv_series_creators (
    series_id  BIGINT  NOT NULL REFERENCES tv_series(id) ON DELETE CASCADE,
    creator_id BIGINT  NOT NULL REFERENCES creators(id) ON DELETE CASCADE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (series_id, creator_id)
);

CREATE TABLE keywords (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_ids JSONB  NOT NULL DEFAULT '{}',
    name         TEXT   NOT NULL DEFAULT ''
);

CREATE TABLE media_keywords (
    media_item_id BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    keyword_id    BIGINT NOT NULL REFERENCES keywords(id) ON DELETE CASCADE,
    PRIMARY KEY (media_item_id, keyword_id)
);

CREATE TABLE production_companies (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_ids   JSONB  NOT NULL DEFAULT '{}',
    name           TEXT NOT NULL DEFAULT '',
    logo_path      TEXT NOT NULL DEFAULT '',
    origin_country TEXT NOT NULL DEFAULT ''
);

CREATE TABLE media_production_companies (
    media_item_id BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    company_id    BIGINT NOT NULL REFERENCES production_companies(id) ON DELETE CASCADE,
    PRIMARY KEY (media_item_id, company_id)
);

CREATE TABLE media_certifications (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT  NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    country       TEXT    NOT NULL DEFAULT '',
    certification TEXT    NOT NULL DEFAULT '',
    release_date  DATE,
    release_type  INTEGER NOT NULL DEFAULT 0,
    source        TEXT    NOT NULL DEFAULT '',
    UNIQUE(media_item_id, country, release_type)
);

CREATE INDEX idx_media_cert_media ON media_certifications (media_item_id);

CREATE TABLE media_videos (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT  NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    provider_key  TEXT    NOT NULL DEFAULT '',
    name          TEXT    NOT NULL DEFAULT '',
    site          TEXT    NOT NULL DEFAULT '',
    video_key     TEXT    NOT NULL DEFAULT '',
    video_type    TEXT    NOT NULL DEFAULT '',
    language      TEXT    NOT NULL DEFAULT '',
    official      BOOLEAN NOT NULL DEFAULT false,
    published_at  TIMESTAMPTZ,
    UNIQUE(media_item_id, video_key)
);

CREATE INDEX idx_media_videos_media ON media_videos (media_item_id);

CREATE TABLE media_recommendations (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id       BIGINT       NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    external_ids        JSONB        NOT NULL DEFAULT '{}',
    title               TEXT         NOT NULL DEFAULT '',
    poster_path         TEXT         NOT NULL DEFAULT '',
    media_type          TEXT         NOT NULL DEFAULT '',
    vote_average        NUMERIC(3,1) NOT NULL DEFAULT 0,
    release_date        TEXT         NOT NULL DEFAULT '',
    UNIQUE(media_item_id, title, media_type)
);

CREATE INDEX idx_media_rec_media ON media_recommendations (media_item_id);

CREATE TABLE external_ratings (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT       NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    source        TEXT         NOT NULL,
    value         TEXT         NOT NULL,
    score         NUMERIC(5,1),
    votes         INTEGER NOT NULL DEFAULT 0,
    raw_value     TEXT    NOT NULL DEFAULT '',
    UNIQUE(media_item_id, source)
);

CREATE INDEX idx_external_ratings_media ON external_ratings (media_item_id);

CREATE TABLE person_biographies (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    language  TEXT NOT NULL,
    biography TEXT NOT NULL DEFAULT '',
    UNIQUE(person_id, language)
);

CREATE INDEX idx_person_bios_person ON person_biographies (person_id);

CREATE TABLE person_profiles (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    person_id  BIGINT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    aspect     TEXT NOT NULL DEFAULT 'profile',
    width      INTEGER NOT NULL DEFAULT 0,
    height     INTEGER NOT NULL DEFAULT 0,
    score      NUMERIC(8,3) NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE(person_id, url)
);

CREATE INDEX idx_person_profiles_person ON person_profiles (person_id);

-- +goose Down
DROP TABLE person_profiles;
DROP TABLE person_biographies;
DROP TABLE external_ratings;
DROP TABLE media_recommendations;
DROP TABLE media_videos;
DROP TABLE media_certifications;
DROP TABLE media_production_companies;
DROP TABLE production_companies;
DROP TABLE media_keywords;
DROP TABLE keywords;
DROP TABLE tv_series_creators;
DROP TABLE creators;
DROP TABLE tv_series_networks;
DROP TABLE networks;
DROP TABLE media_crew;
DROP TABLE media_cast;
DROP TABLE people;
