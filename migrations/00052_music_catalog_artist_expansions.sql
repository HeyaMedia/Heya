-- +goose Up

-- Tracks which unowned similar-artist neighborhoods have already contributed
-- their bounded top-track set. This prevents every embedding sweep from
-- refetching the same external artist while still allowing a monthly refresh.
CREATE TABLE public.music_catalog_artist_expansions (
    source_artist_id bigint NOT NULL REFERENCES public.artists(id) ON DELETE CASCADE,
    related_artist_mbid text NOT NULL,
    related_artist_name text NOT NULL DEFAULT '',
    hydrated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_artist_id, related_artist_mbid)
);

CREATE INDEX music_catalog_artist_expansions_hydrated_idx
    ON public.music_catalog_artist_expansions (hydrated_at);

-- +goose Down

DROP INDEX IF EXISTS public.music_catalog_artist_expansions_hydrated_idx;
DROP TABLE IF EXISTS public.music_catalog_artist_expansions;
