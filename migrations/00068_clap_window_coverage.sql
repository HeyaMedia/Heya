-- +goose Up

-- The original CLAP pipeline stored one normalized embedding from the middle
-- ten seconds of each track. Track how many representative windows have been
-- folded into the persisted embedding so existing rows can be augmented
-- without rerunning Discogs/BPM/key analysis.
ALTER TABLE public.track_facets
    ADD COLUMN clap_windows smallint NOT NULL DEFAULT 0,
    ADD CONSTRAINT track_facets_clap_windows_check
        CHECK (clap_windows BETWEEN 0 AND 3);

-- A real legacy embedding represents the center window. Failure stubs contain
-- no embedding and are marked current so they keep their existing
-- do-not-retry-until-analyzer-version-bumps behaviour.
UPDATE public.track_facets
SET clap_windows = CASE
    WHEN text_embedding IS NULL THEN 3
    ELSE 1
END;

-- +goose Down

ALTER TABLE public.track_facets
    DROP CONSTRAINT track_facets_clap_windows_check,
    DROP COLUMN clap_windows;
