-- +goose Up
ALTER TABLE tv_episodes ADD COLUMN rating NUMERIC(3,1) NOT NULL DEFAULT 0;
ALTER TABLE tv_episodes ADD COLUMN vote_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE tv_episodes DROP COLUMN vote_count;
ALTER TABLE tv_episodes DROP COLUMN rating;
