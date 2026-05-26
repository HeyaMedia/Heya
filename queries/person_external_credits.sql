-- name: UpsertPersonExternalCredit :exec
INSERT INTO person_external_credits (
    person_id, kind, media_kind, title, year, character, job, department,
    episode_count, display_order, slug, poster_url, external_ids, source
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (person_id, kind, title, year, character, job) DO UPDATE SET
    media_kind    = EXCLUDED.media_kind,
    department    = EXCLUDED.department,
    episode_count = EXCLUDED.episode_count,
    display_order = EXCLUDED.display_order,
    slug          = EXCLUDED.slug,
    poster_url    = EXCLUDED.poster_url,
    external_ids  = EXCLUDED.external_ids,
    source        = EXCLUDED.source;

-- name: DeletePersonExternalCredits :exec
DELETE FROM person_external_credits WHERE person_id = $1;

-- ListPersonExternalCredits returns every external credit for a person and
-- pairs it with the matching media_items row (if any) so the FE can show
-- whether each credit is already in the local library. We use scalar
-- subqueries (rather than a LEFT JOIN) so sqlc infers nullable types for
-- the matched columns — a LEFT JOIN with column refs would otherwise be
-- typed non-nullable and scan-fail on NULL.
--
-- The JSONB equality check matches the credit's recorded external_ids
-- against the library's media_items.external_ids one provider at a time
-- (tmdb / tvdb / imdb). Any single match counts; we take the first.
-- name: ListPersonExternalCredits :many
SELECT
    ec.id,
    ec.person_id,
    ec.kind,
    ec.media_kind,
    ec.title,
    ec.year,
    ec.character,
    ec.job,
    ec.department,
    ec.episode_count,
    ec.display_order,
    ec.slug,
    ec.poster_url,
    ec.external_ids,
    ec.source,
    -- Explicit BIGINT/TEXT casts on the COALESCE so sqlc generates
    -- concrete int64/string types instead of interface{} (which would
    -- break the `!= 0` / `!= ""` checks at the call site). Callers
    -- treat `matched_media_item_id == 0` as "no library match".
    COALESCE((SELECT mi.id FROM media_items mi WHERE
        (ec.external_ids ? 'tmdb' AND mi.external_ids ->> 'tmdb' = ec.external_ids ->> 'tmdb')
        OR (ec.external_ids ? 'tvdb' AND mi.external_ids ->> 'tvdb' = ec.external_ids ->> 'tvdb')
        OR (ec.external_ids ? 'imdb' AND mi.external_ids ->> 'imdb' = ec.external_ids ->> 'imdb')
        LIMIT 1), 0)::BIGINT         AS matched_media_item_id,
    COALESCE((SELECT mi.slug FROM media_items mi WHERE
        (ec.external_ids ? 'tmdb' AND mi.external_ids ->> 'tmdb' = ec.external_ids ->> 'tmdb')
        OR (ec.external_ids ? 'tvdb' AND mi.external_ids ->> 'tvdb' = ec.external_ids ->> 'tvdb')
        OR (ec.external_ids ? 'imdb' AND mi.external_ids ->> 'imdb' = ec.external_ids ->> 'imdb')
        LIMIT 1), '')::TEXT          AS matched_slug,
    COALESCE((SELECT mi.media_type::TEXT FROM media_items mi WHERE
        (ec.external_ids ? 'tmdb' AND mi.external_ids ->> 'tmdb' = ec.external_ids ->> 'tmdb')
        OR (ec.external_ids ? 'tvdb' AND mi.external_ids ->> 'tvdb' = ec.external_ids ->> 'tvdb')
        OR (ec.external_ids ? 'imdb' AND mi.external_ids ->> 'imdb' = ec.external_ids ->> 'imdb')
        LIMIT 1), '')::TEXT          AS matched_media_type
FROM person_external_credits ec
WHERE ec.person_id = $1
ORDER BY ec.kind, ec.display_order, ec.year DESC NULLS LAST, ec.title;
