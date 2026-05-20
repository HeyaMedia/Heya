-- +goose Up
DELETE FROM media_extras a USING media_extras b
WHERE a.id > b.id AND a.media_item_id = b.media_item_id AND a.extra_type = b.extra_type AND a.title = b.title;

CREATE UNIQUE INDEX IF NOT EXISTS idx_media_extras_unique ON media_extras (media_item_id, extra_type, title);

-- +goose Down
DROP INDEX IF EXISTS idx_media_extras_unique;
