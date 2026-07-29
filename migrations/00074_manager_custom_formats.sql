-- +goose Up
-- Custom formats: TRaSH-compatible release conditions the decision engine
-- scores against. Specifications keep the arr implementation names but store
-- canonical string values (enums are resolved at import time, so a stored
-- spec means the same thing whether it came from Radarr, Sonarr, or a paste).
CREATE TABLE manager_custom_formats (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    name text NOT NULL,
    -- 'video' | 'music' | 'book' — which library domain the format applies to
    domain text DEFAULT 'video'::text NOT NULL,
    include_when_renaming boolean DEFAULT false NOT NULL,
    -- [{"name","implementation","negate","required","fields":{"value":…}}]
    specifications jsonb DEFAULT '[]'::jsonb NOT NULL,
    -- TRaSH guides identity when known; lets a future re-sync update in place
    trash_id text DEFAULT ''::text NOT NULL,
    -- '' manual, else the importer that produced it: 'radarr'|'sonarr'|'lidarr'|'trash'
    source text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT manager_custom_formats_pkey PRIMARY KEY (id),
    CONSTRAINT manager_custom_formats_domain_name_key UNIQUE (domain, name)
);

-- Profiles gain the format-score dimension: a release must clear
-- min_format_score to be grabbed at all, upgrades stop once
-- cutoff_format_score is reached, and a new grab must beat the current one
-- by at least min_upgrade_score. format_scores holds only non-zero entries:
-- [{"format_id": 12, "score": 2000}, …].
ALTER TABLE manager_quality_profiles
    ADD COLUMN min_format_score integer DEFAULT 0 NOT NULL,
    ADD COLUMN cutoff_format_score integer DEFAULT 0 NOT NULL,
    ADD COLUMN min_upgrade_score integer DEFAULT 1 NOT NULL,
    ADD COLUMN format_scores jsonb DEFAULT '[]'::jsonb NOT NULL;

-- +goose Down
ALTER TABLE manager_quality_profiles
    DROP COLUMN IF EXISTS min_format_score,
    DROP COLUMN IF EXISTS cutoff_format_score,
    DROP COLUMN IF EXISTS min_upgrade_score,
    DROP COLUMN IF EXISTS format_scores;
DROP TABLE IF EXISTS manager_custom_formats;
