-- +goose Up

-- heya.media returns track durations in seconds (e.g. duration: 238 for ~4 min).
-- Rename the column to match the unit so we don't accidentally store seconds in
-- a field named "_ms".
ALTER TABLE tracks RENAME COLUMN duration_ms TO duration;

-- +goose Down
ALTER TABLE tracks RENAME COLUMN duration TO duration_ms;
