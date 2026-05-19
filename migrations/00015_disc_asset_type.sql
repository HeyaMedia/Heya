-- +goose Up
ALTER TYPE asset_type ADD VALUE IF NOT EXISTS 'disc';

-- +goose Down
-- Cannot remove enum values in PostgreSQL
