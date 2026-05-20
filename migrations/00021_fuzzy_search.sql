-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- +goose StatementEnd

-- Immutable wrapper for array_to_string (required for generated columns)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION immutable_array_to_string(arr text[], sep text)
RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT array_to_string(arr, sep)
$$;
-- +goose StatementEnd

-- People: searchable name + biography, trigram fuzzy match on name
ALTER TABLE people ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('simple', immutable_array_to_string(coalesce(also_known_as, '{}'::text[]), ' ')), 'B') ||
        setweight(to_tsvector('english', coalesce(biography, '')), 'D')
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_people_search ON people USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_people_name_trgm ON people USING GIN (lower(name) gin_trgm_ops);

-- Albums: searchable title, trigram fuzzy match on title
ALTER TABLE albums ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('simple', immutable_array_to_string(coalesce(tags, '{}'::text[]), ' ')), 'C')
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_albums_search ON albums USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_albums_title_trgm ON albums USING GIN (lower(title) gin_trgm_ops);

-- Tracks: searchable title, trigram fuzzy match
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(title, ''))
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_tracks_search ON tracks USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_tracks_title_trgm ON tracks USING GIN (lower(title) gin_trgm_ops);

-- Collections: searchable name + overview
ALTER TABLE collections ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(overview, '')), 'D')
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_collections_search ON collections USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_collections_name_trgm ON collections USING GIN (lower(name) gin_trgm_ops);

-- Authors: searchable name + biography
ALTER TABLE authors ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', coalesce(name, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(biography, '')), 'D')
    ) STORED;
CREATE INDEX IF NOT EXISTS idx_authors_search ON authors USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_authors_name_trgm ON authors USING GIN (lower(name) gin_trgm_ops);

-- Trigram index on media_items.title (full-text already exists on search_vector)
CREATE INDEX IF NOT EXISTS idx_media_items_title_trgm ON media_items USING GIN (lower(title) gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_media_items_title_trgm;

DROP INDEX IF EXISTS idx_authors_name_trgm;
DROP INDEX IF EXISTS idx_authors_search;
ALTER TABLE authors DROP COLUMN IF EXISTS search_vector;

DROP INDEX IF EXISTS idx_collections_name_trgm;
DROP INDEX IF EXISTS idx_collections_search;
ALTER TABLE collections DROP COLUMN IF EXISTS search_vector;

DROP INDEX IF EXISTS idx_tracks_title_trgm;
DROP INDEX IF EXISTS idx_tracks_search;
ALTER TABLE tracks DROP COLUMN IF EXISTS search_vector;

DROP INDEX IF EXISTS idx_albums_title_trgm;
DROP INDEX IF EXISTS idx_albums_search;
ALTER TABLE albums DROP COLUMN IF EXISTS search_vector;

DROP INDEX IF EXISTS idx_people_name_trgm;
DROP INDEX IF EXISTS idx_people_search;
ALTER TABLE people DROP COLUMN IF EXISTS search_vector;

DROP FUNCTION IF EXISTS immutable_array_to_string(text[], text);
