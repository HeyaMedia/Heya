-- +goose Up
ALTER TABLE media_items ADD COLUMN IF NOT EXISTS slug TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_items_slug ON media_items(slug) WHERE slug != '';

ALTER TABLE people ADD COLUMN IF NOT EXISTS slug TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_people_slug ON people(slug) WHERE slug != '';

-- +goose Down
DROP INDEX IF EXISTS idx_people_slug;
ALTER TABLE people DROP COLUMN IF EXISTS slug;
DROP INDEX IF EXISTS idx_media_items_slug;
ALTER TABLE media_items DROP COLUMN IF EXISTS slug;
