-- +goose Up
ALTER TABLE library_files ADD COLUMN keyframes JSONB;

-- +goose Down
ALTER TABLE library_files DROP COLUMN IF EXISTS keyframes;
