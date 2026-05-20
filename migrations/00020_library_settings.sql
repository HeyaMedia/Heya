-- +goose Up
ALTER TABLE libraries ADD COLUMN settings JSONB NOT NULL DEFAULT '{}';

CREATE TABLE external_ratings (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    media_item_id BIGINT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    source        TEXT   NOT NULL,
    value         TEXT   NOT NULL,
    score         NUMERIC(5,1),
    UNIQUE(media_item_id, source)
);

CREATE INDEX idx_external_ratings_media ON external_ratings(media_item_id);

ALTER TABLE media_items ADD COLUMN metadata_refreshed_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE media_items DROP COLUMN IF EXISTS metadata_refreshed_at;
DROP INDEX IF EXISTS idx_external_ratings_media;
DROP TABLE IF EXISTS external_ratings;
ALTER TABLE libraries DROP COLUMN IF EXISTS settings;
