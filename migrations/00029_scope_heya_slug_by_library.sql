-- +goose Up
DROP INDEX IF EXISTS idx_media_items_heya_slug;
CREATE UNIQUE INDEX idx_media_items_heya_slug ON media_items (library_id, heya_slug) WHERE heya_slug != '';

-- +goose Down
-- Global heya_slug uniqueness cannot be safely restored once different
-- libraries are allowed to share a slug. Keep only the lookup index on
-- rollback; old app builds should be run on a wiped DB if they require the
-- historical global uniqueness contract.
DROP INDEX IF EXISTS idx_media_items_heya_slug;
CREATE INDEX idx_media_items_heya_slug ON media_items (heya_slug) WHERE heya_slug != '';
