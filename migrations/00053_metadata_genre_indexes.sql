-- +goose Up

-- Case-folded array wrapper for expression indexes: lower() applied per
-- element. Mirrors immutable_array_to_string — plain lower() over an array
-- doesn't exist, and the fold must be IMMUTABLE to be indexable.
-- +goose StatementBegin
CREATE FUNCTION public.immutable_lower_array(arr text[]) RETURNS text[]
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT coalesce(array_agg(lower(x)), '{}'::text[]) FROM unnest(arr) AS x
$$;
-- +goose StatementEnd

-- Browse-by-metadata-genre lookups (artist-hero genre chips → drilldown):
-- resolve "which artists/albums carry this genre or tag, case-insensitively"
-- with a GIN containment probe instead of seq-scanning both tables with a
-- per-row unnest filter (~200ms on prod). ANALYZE also collects
-- most-common-element stats on the indexed expression, so the planner stops
-- guessing 50% selectivity for tag matches.
CREATE INDEX idx_artists_genres_tags_lower
    ON public.artists USING gin (public.immutable_lower_array(genres || tags));
CREATE INDEX idx_albums_genres_tags_lower
    ON public.albums USING gin (public.immutable_lower_array(genres || tags));

-- +goose Down

DROP INDEX IF EXISTS public.idx_albums_genres_tags_lower;
DROP INDEX IF EXISTS public.idx_artists_genres_tags_lower;
DROP FUNCTION IF EXISTS public.immutable_lower_array(text[]);
