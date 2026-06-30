-- name: CreateMediaItem :one
INSERT INTO media_items (library_id, media_type, title, sort_title, year, description, poster_path, backdrop_path, external_ids, tagline, original_title, original_language, status, provider_kind, heya_slug)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
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
-- "matched, awaiting enrich" (pending) bucket while still being visible; the
-- local_identity_key anchors re-scan dedup for NFO-less items.
UPDATE media_items
SET enrichment_status  = 'local',
    local_identity_key = $2,
    match_confidence   = $3
WHERE id = $1;

-- name: GetMediaItemByLocalIdentityKey :one
-- Dedup lookup for NFO-less locally-materialized entities. Keyed on the
-- normalized lower(title)|year|media_type so a re-scan links to the same row
-- instead of relying on external_ids containment (which mis-joins on '{}').
SELECT * FROM media_items
WHERE library_id = $1 AND local_identity_key = sqlc.arg(local_identity_key)
  AND local_identity_key != ''
ORDER BY id
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

-- name: ListEnrichedMovies :many
SELECT mi.id, mi.library_id, mi.media_type, mi.title, mi.sort_title, mi.slug,
       mi.year, mi.description, mi.poster_path, mi.backdrop_path,
       mi.external_ids, mi.created_at, mi.updated_at,
       m.genres, m.rating, m.runtime_minutes, m.original_language,
       m.release_date, m.collection_id
FROM media_items mi
JOIN movies m ON m.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;

-- name: ListEnrichedTVSeries :many
SELECT mi.id, mi.library_id, mi.media_type, mi.title, mi.sort_title, mi.slug,
       mi.year, mi.description, mi.poster_path, mi.backdrop_path,
       mi.external_ids, mi.created_at, mi.updated_at,
       ts.genres, ts.rating, ts.first_air_date, ts.last_air_date,
       ts.status, ts.original_language, ts.number_of_seasons, ts.number_of_episodes
FROM media_items mi
JOIN tv_series ts ON ts.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;
