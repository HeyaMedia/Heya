-- name: CreateMediaItem :one
-- enrichment_status is set explicitly (not left to the column default) so the
-- local-materialize path can stamp 'local' AT INSERT — that's what makes the
-- partial unique index idx_media_items_local_identity (WHERE
-- enrichment_status='local') catch the concurrent-materialize race at create
-- time. Non-local callers pass 'pending' (the prior default).
INSERT INTO media_items (library_id, media_type, title, sort_title, year, description, poster_path, backdrop_path, external_ids, tagline, original_title, original_language, status, provider_kind, heya_slug, enrichment_status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, sqlc.arg(enrichment_status))
RETURNING *;

-- name: GetMediaItemByID :one
SELECT * FROM media_items WHERE id = $1;

-- name: GetMediaItemBySlug :one
SELECT * FROM media_items WHERE slug = $1;

-- name: UpdateMediaItemSlug :exec
UPDATE media_items SET slug = $2 WHERE id = $1;

-- name: UpdateMediaItemHeyaSlug :exec
-- Writes the canonical heya.media slug back onto the media_item.
-- Called by the enrich workers after GetDetail returns — the slug is
-- a stable lookup key for future re-fetches (heya.media supports
-- slug:<slug> as an artist lookup ID alongside mbid:<id> and the
-- per-provider variants). Distinct from `slug` which is our own
-- user-facing URL identifier.
UPDATE media_items SET heya_slug = $2, updated_at = now() WHERE id = $1;

-- name: MediaItemSlugExists :one
SELECT EXISTS(SELECT 1 FROM media_items WHERE slug = $1 AND id != $2) as exists;

-- name: ListMediaItemsByLibrary :many
SELECT * FROM media_items
WHERE library_id = $1
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: ListMediaItemsByType :many
SELECT * FROM media_items
WHERE media_type = $1
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: ListMediaItemsByTypeRecent :many
-- Same page shape as ListMediaItemsByType but newest-first — powers the
-- home "Recently Added" rails (created_at is when the first file matched).
SELECT * FROM media_items
WHERE media_type = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: UpdateMediaItem :one
UPDATE media_items
SET title = $2, sort_title = $3, year = $4, description = $5,
    poster_path = $6, backdrop_path = $7, external_ids = $8,
    tagline = $9, original_title = $10, original_language = $11,
    status = $12, provider_kind = $13, heya_slug = $14, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMediaItem :exec
DELETE FROM media_items WHERE id = $1;

-- name: CountMediaItemsByLibrary :one
SELECT count(*) FROM media_items WHERE library_id = $1;

-- name: CountMediaItemsByType :one
SELECT count(*) FROM media_items WHERE media_type = $1;

-- name: MarkMetadataRefreshed :exec
UPDATE media_items SET metadata_refreshed_at = now() WHERE id = $1;

-- name: MarkMediaItemLocal :exec
-- Flags a media_item as born from local signal (NFO/tags/filename) with no
-- confident remote match yet. enrichment_status='local' keeps it out of the
-- "matched, awaiting enrich" (pending) bucket while still being visible. Re-scan
-- dedup is handled by natural identity (FindMediaItemByIdentity), not a stored
-- key, so nothing is written here to anchor it.
UPDATE media_items
SET enrichment_status = 'local',
    match_confidence  = $2
WHERE id = $1;

-- name: FindMediaItemByIdentity :one
-- Dedup by natural identity: normalized lower(btrim(title)) | year | media_type,
-- scoped to the library. Prefers the enriched, then oldest, row as canonical.
-- Backed by idx_media_items_identity (library_id, media_type, year,
-- lower(btrim(title))); the probe normalizes with the same SQL so it matches.
--
-- include_matched gates how far the fold reaches:
--   true  — any row (enriched/complete included). Used ONLY when remote search
--           returned nothing, so a genuine heya.media outage on a new file folds
--           it into the existing show rather than forking a duplicate.
--   false — local stubs only (enrichment_status='local'). Used on the ambiguous
--           "needs review" path: re-scan dedup still works, but a coincidental
--           title+year collision can NEVER silently attach onto a published,
--           remotely-matched item.
SELECT * FROM media_items
WHERE library_id = $1
  AND media_type = sqlc.arg(media_type)
  AND year       = sqlc.arg(year)
  AND lower(btrim(title)) = lower(btrim(sqlc.arg(title)))
  AND (sqlc.arg(include_matched)::boolean OR enrichment_status = 'local')
ORDER BY (enrichment_status = 'local') ASC, id ASC
LIMIT 1;

-- name: FindLocalMediaItemByIdentity :one
-- The enrichment_status='local' counterpart of FindMediaItemByIdentity, for the
-- matcher's create-time retry: when a concurrent metadata_match worker wins the
-- natural-identity race (23505 on idx_media_items_local_identity), re-resolve
-- the winner by the exact predicate the partial unique index enforces so the
-- loser's file links to it. Scoped to un-enriched local stubs ('local'), which
-- the local path now stamps at INSERT, so it resolves the winner the instant its
-- row commits — the moment the conflict fires. Enriched items ('complete') have
-- left the index and are never matched here.
SELECT * FROM media_items
WHERE library_id = $1
  AND media_type = sqlc.arg(media_type)
  AND year       = sqlc.arg(year)
  AND lower(btrim(title)) = lower(btrim(sqlc.arg(title)))
  AND enrichment_status = 'local'
ORDER BY id ASC
LIMIT 1;

-- name: SetMediaItemFieldProvenance :exec
-- Replaces the per-field provenance map ({field: "local"|"remote"|"user"}).
-- Written by the matcher (stamping locally-derived fields 'local') and the
-- metadata editor (stamping user-edited fields 'user'); read by the enrich
-- writers so they fill local/empty fields but never overwrite a user edit.
UPDATE media_items SET field_provenance = $2 WHERE id = $1;

-- name: MarkMatched :exec
-- Stamped by the match step after writing a search-only stub. Sets the
-- enrichment_status floor to 'pending' (it's the default for new rows, but
-- this also covers re-match flows that may have advanced it).
UPDATE media_items
   SET matched_at        = now(),
       enrichment_status = CASE WHEN enrichment_status = '' THEN 'pending' ELSE enrichment_status END
 WHERE id = $1;

-- name: MarkEnrichAttempted :exec
-- Called at the start of an enrich attempt. Clears any prior error so a
-- successful run leaves last_enrich_error empty.
UPDATE media_items
   SET last_enrich_attempt_at = now(),
       last_enrich_error      = ''
 WHERE id = $1;

-- name: MarkEnrichFailed :exec
-- Called when an enrich attempt errors. The status flips to 'failed' so the
-- UI surfaces it; the worker is free to retry on a subsequent run.
UPDATE media_items
   SET enrichment_status      = 'failed',
       last_enrich_error      = $2,
       last_enrich_attempt_at = now()
 WHERE id = $1;

-- name: MarkEnrichBaseDone :exec
-- Type-specific row + base fields populated.
UPDATE media_items SET base_enriched_at = now() WHERE id = $1;

-- name: MarkEnrichPeopleDone :exec
-- Cast + crew rows populated.
UPDATE media_items SET people_enriched_at = now() WHERE id = $1;

-- name: MarkEnrichExtrasDone :exec
-- Keywords + videos + recommendations + certifications + alt-titles populated.
UPDATE media_items SET extras_enriched_at = now() WHERE id = $1;

-- name: MarkEnrichImagesDone :exec
-- Image downloads have been enqueued (URLs known, local cache pending).
UPDATE media_items SET images_enriched_at = now() WHERE id = $1;

-- name: MarkEnrichStructureDone :exec
-- TV seasons + episodes tree built. No-op for movies / books / music.
UPDATE media_items SET structure_enriched_at = now() WHERE id = $1;

-- name: MarkEnrichComplete :exec
-- Final stamp at the end of a successful enrich. Flips status to 'complete'
-- and updates metadata_refreshed_at (the legacy stale-detection timestamp).
UPDATE media_items
   SET enrichment_status      = 'complete',
       metadata_refreshed_at  = now()
 WHERE id = $1;

-- name: MarkEnrichPartial :exec
-- Used when at least one component has landed but more remain. The worker
-- only calls this if MarkEnrichComplete won't fire on this run.
UPDATE media_items
   SET enrichment_status = 'partial'
 WHERE id = $1;

-- name: ListUnavailableMediaItemIDs :many
SELECT DISTINCT mi.id
FROM media_items mi
WHERE mi.media_type = $1
  AND NOT EXISTS (
    SELECT 1 FROM library_files lf
    WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
  );

-- name: ListUnavailableMediaItemIDsForItems :many
-- Page-scoped availability: given only the media_item IDs actually returned to
-- the caller, report which have no live files on disk. The unscoped
-- ListUnavailableMediaItemIDs above scans the entire media type on every list /
-- rail load (twice per dashboard); this bounds the anti-join to the visible page.
SELECT mi.id
FROM media_items mi
WHERE mi.id = ANY(sqlc.arg(ids)::bigint[])
  AND NOT EXISTS (
    SELECT 1 FROM library_files lf
    WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
  );

-- name: SearchMediaItemsByLibrary :many
SELECT * FROM media_items
WHERE library_id = $1
  AND ($4::text = '' OR title ILIKE '%' || $4 || '%')
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: UpdateMediaItemPosterPath :exec
UPDATE media_items SET poster_path = $2, updated_at = now() WHERE id = $1;

-- name: UpdateMediaItemBackdropPath :exec
UPDATE media_items SET backdrop_path = $2, updated_at = now() WHERE id = $1;

-- mi.description holds the provider's base (English) overview; rows enriched
-- before that field existed have it empty, with only media_overviews
-- translations (which exclude the base language). The browse detail view
-- wants one synopsis line: description, else the 'en' overview, else the
-- item's original-language overview. Deliberately NOT "any language" — a
-- random-alphabet synopsis is worse than none, and a metadata refresh
-- backfills description anyway.
-- name: ListEnrichedMovies :many
SELECT mi.id, mi.library_id, mi.media_type, mi.title, mi.sort_title, mi.slug,
       mi.year,
       COALESCE(NULLIF(mi.description, ''),
         (SELECT mo.overview FROM media_overviews mo
          WHERE mo.media_item_id = mi.id AND mo.language = 'en' LIMIT 1),
         (SELECT mo.overview FROM media_overviews mo
          WHERE mo.media_item_id = mi.id AND mo.language = m.original_language LIMIT 1),
         '')::text AS description,
       mi.poster_path, mi.backdrop_path,
       mi.external_ids, mi.created_at, mi.updated_at,
       m.genres, m.rating, m.runtime_minutes, m.original_language,
       m.release_date, m.collection_id
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;

-- name: ListEnrichedTVSeries :many
SELECT mi.id, mi.library_id, mi.media_type, mi.title, mi.sort_title, mi.slug,
       mi.year,
       COALESCE(NULLIF(mi.description, ''),
         (SELECT mo.overview FROM media_overviews mo
          WHERE mo.media_item_id = mi.id AND mo.language = 'en' LIMIT 1),
         (SELECT mo.overview FROM media_overviews mo
          WHERE mo.media_item_id = mi.id AND mo.language = ts.original_language LIMIT 1),
         '')::text AS description,
       mi.poster_path, mi.backdrop_path,
       mi.external_ids, mi.created_at, mi.updated_at,
       ts.genres, ts.rating, ts.first_air_date, ts.last_air_date,
       ts.status, ts.original_language, ts.number_of_seasons, ts.number_of_episodes
FROM media_items mi
JOIN tv_series ts ON ts.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;
