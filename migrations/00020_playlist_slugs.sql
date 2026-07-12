-- +goose Up

-- User-facing playlist URLs (mirrors media items / albums: a stable slug
-- column instead of ID-addressing). Backfill derives a slug from the
-- existing name so pre-existing playlists get a real URL on upgrade rather
-- than staying on ID-only routing.
ALTER TABLE public.user_playlists ADD COLUMN slug text NOT NULL DEFAULT '';

-- Backfill: slugify each playlist's current name, then de-dupe within each
-- user by suffixing "-<id>" onto every collision but the first (oldest by
-- id) row — mirrors the ExistsFunc collision-suffix scheme in
-- internal/slug.GenerateUnique, just computed in bulk instead of one row at
-- a time. Blank/punctuation-only names (edge case; playlist names aren't
-- otherwise validated to be sluggable) fall back to "playlist-<id>" so we
-- never produce an empty slug.
WITH base_slugs AS (
    SELECT id,
           user_id,
           NULLIF(trim(both '-' from lower(regexp_replace(name, '[^a-zA-Z0-9]+', '-', 'g'))), '') AS base_slug
    FROM public.user_playlists
),
ranked AS (
    SELECT id,
           user_id,
           coalesce(base_slug, 'playlist-' || id::text) AS base_slug,
           row_number() OVER (
               PARTITION BY user_id, coalesce(base_slug, 'playlist-' || id::text)
               ORDER BY id
           ) AS rn
    FROM base_slugs
)
UPDATE public.user_playlists up
SET slug = CASE WHEN ranked.rn = 1 THEN ranked.base_slug ELSE ranked.base_slug || '-' || ranked.id::text END
FROM ranked
WHERE up.id = ranked.id;

CREATE UNIQUE INDEX user_playlists_user_id_slug_idx ON public.user_playlists (user_id, slug);

-- +goose Down

DROP INDEX IF EXISTS public.user_playlists_user_id_slug_idx;
ALTER TABLE public.user_playlists DROP COLUMN IF EXISTS slug;
