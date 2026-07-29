-- +goose Up
-- Provenance for imported quality profiles: which arr app a profile was
-- imported from ('' for seeds and hand-made rows). Re-importing from the
-- same app updates its profiles in place instead of stacking suffixed
-- duplicates.
ALTER TABLE manager_quality_profiles
    ADD COLUMN source text DEFAULT ''::text NOT NULL;

-- +goose Down
ALTER TABLE manager_quality_profiles
    DROP COLUMN IF EXISTS source;
