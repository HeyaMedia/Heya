-- +goose Up
-- Granular TV monitoring for the acquisition pipeline. Effective monitoring
-- = item AND season AND episode; specials default unmonitored (Sonarr
-- convention) — a user flipping one on makes it wanted like any episode.
ALTER TABLE tv_seasons ADD COLUMN monitored boolean NOT NULL DEFAULT true;
ALTER TABLE tv_episodes ADD COLUMN monitored boolean NOT NULL DEFAULT true;

UPDATE tv_episodes SET monitored = false WHERE is_special;
UPDATE tv_seasons SET monitored = false WHERE season_number = 0;

-- +goose Down
ALTER TABLE tv_episodes DROP COLUMN monitored;
ALTER TABLE tv_seasons DROP COLUMN monitored;
