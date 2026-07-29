-- +goose Up
-- Radarr-parity language gate on quality profiles: 'any' (no filter),
-- 'original' (the media item's original audio), or a language name. This is
-- a hard filter the decision engine applies before scoring; language
-- *preference* stays in custom-format scores (the Sonarr model).
ALTER TABLE manager_quality_profiles
    ADD COLUMN language text DEFAULT 'any'::text NOT NULL;

-- +goose Down
ALTER TABLE manager_quality_profiles
    DROP COLUMN IF EXISTS language;
