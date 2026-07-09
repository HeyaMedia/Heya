-- name: CreateMediaItemRaw :one
WITH entity AS (
  INSERT INTO media_items (library_id, media_type, provider_kind, heya_slug)
  VALUES (
    sqlc.arg(library_id),
    sqlc.arg(media_type),
    sqlc.arg(provider_kind),
    sqlc.arg(heya_slug)
  )
  RETURNING id
),
profile AS (
  INSERT INTO media_item_profiles (
    media_item_id, title, sort_title, year, description, poster_path,
    backdrop_path, tagline, original_title, original_language, status
  )
  SELECT
    entity.id,
    sqlc.arg(title),
    sqlc.arg(sort_title),
    sqlc.arg(year),
    sqlc.arg(description),
    sqlc.arg(poster_path),
    sqlc.arg(backdrop_path),
    sqlc.arg(tagline),
    sqlc.arg(original_title),
    sqlc.arg(original_language),
    sqlc.arg(status)
  FROM entity
  RETURNING media_item_id
),
external_ids AS (
  INSERT INTO media_item_external_ids (media_item_id, library_id, provider, external_id, source)
  SELECT entity.id, sqlc.arg(library_id), kv.key, kv.value, 'media_items.external_ids'
  FROM entity, jsonb_each_text(
    CASE
      WHEN jsonb_typeof(sqlc.arg(external_ids)::jsonb) = 'object' THEN sqlc.arg(external_ids)::jsonb
      ELSE '{}'::jsonb
    END
  ) AS kv(key, value)
  WHERE kv.key <> '' AND kv.value <> ''
  ON CONFLICT (media_item_id, provider) DO UPDATE SET
    library_id = EXCLUDED.library_id,
    external_id = EXCLUDED.external_id,
    source = EXCLUDED.source,
    updated_at = now()
  RETURNING provider
)
SELECT entity.id
FROM entity
JOIN profile ON profile.media_item_id = entity.id
CROSS JOIN (SELECT count(*) FROM external_ids) external_write_count;

-- name: GetMediaItemByID :one
SELECT * FROM media_item_cards WHERE id = $1;

-- name: GetMediaItemBySlug :one
SELECT * FROM media_item_cards WHERE slug = $1;

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
SELECT * FROM media_item_cards
WHERE library_id = $1
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: ListMediaItemsByType :many
SELECT * FROM media_item_cards
WHERE media_type = $1
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: ListMediaItemsByTypeRecent :many
-- Same page shape as ListMediaItemsByType but newest-first — powers the
-- home "Recently Added" rails (created_at is when the first file matched).
SELECT * FROM media_item_cards
WHERE media_type = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: UpdateMediaItemRaw :one
WITH entity AS (
  UPDATE media_items
     SET provider_kind = sqlc.arg(provider_kind),
         heya_slug = sqlc.arg(heya_slug),
         updated_at = now()
   WHERE media_items.id = sqlc.arg(id)
   RETURNING *
),
profile AS (
  INSERT INTO media_item_profiles (
    media_item_id, title, sort_title, year, description, poster_path,
    backdrop_path, tagline, original_title, original_language, status, updated_at
  )
  SELECT
    entity.id,
    sqlc.arg(title),
    sqlc.arg(sort_title),
    sqlc.arg(year),
    sqlc.arg(description),
    sqlc.arg(poster_path),
    sqlc.arg(backdrop_path),
    sqlc.arg(tagline),
    sqlc.arg(original_title),
    sqlc.arg(original_language),
    sqlc.arg(status),
    now()
  FROM entity
  ON CONFLICT (media_item_id) DO UPDATE SET
    title = EXCLUDED.title,
    sort_title = EXCLUDED.sort_title,
    year = EXCLUDED.year,
    description = EXCLUDED.description,
    poster_path = EXCLUDED.poster_path,
    backdrop_path = EXCLUDED.backdrop_path,
    tagline = EXCLUDED.tagline,
    original_title = EXCLUDED.original_title,
    original_language = EXCLUDED.original_language,
    status = EXCLUDED.status,
    updated_at = now()
  RETURNING media_item_id
),
desired_external_ids AS (
  SELECT kv.key AS provider, kv.value AS external_id
  FROM jsonb_each_text(
    CASE
      WHEN jsonb_typeof(sqlc.arg(external_ids)::jsonb) = 'object' THEN sqlc.arg(external_ids)::jsonb
      ELSE '{}'::jsonb
    END
  ) AS kv(key, value)
  WHERE kv.key <> '' AND kv.value <> ''
),
inserted_external_ids AS (
  INSERT INTO media_item_external_ids (media_item_id, library_id, provider, external_id, source)
  SELECT entity.id, entity.library_id, desired.provider, desired.external_id, 'media_items.external_ids'
  FROM entity, desired_external_ids desired
  ON CONFLICT (media_item_id, provider) DO UPDATE SET
    library_id = EXCLUDED.library_id,
    external_id = EXCLUDED.external_id,
    source = EXCLUDED.source,
    updated_at = now()
  RETURNING provider
),
deleted_external_ids AS (
  DELETE FROM media_item_external_ids existing
  WHERE existing.media_item_id = sqlc.arg(id)
    AND NOT EXISTS (
      SELECT 1
      FROM desired_external_ids desired
      WHERE desired.provider = existing.provider
    )
  RETURNING 1
)
SELECT entity.id
FROM entity
JOIN profile ON profile.media_item_id = entity.id
CROSS JOIN (SELECT count(*) FROM deleted_external_ids) deleted_write_count
CROSS JOIN (SELECT count(*) FROM inserted_external_ids) inserted_write_count;

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
SELECT * FROM media_item_cards
WHERE library_id = $1
  AND media_type = sqlc.arg(media_type)
  AND year       = sqlc.arg(year)
  AND lower(btrim(title)) = lower(btrim(sqlc.arg(title)))
  AND (sqlc.arg(include_matched)::boolean OR enrichment_status = 'local')
ORDER BY (enrichment_status = 'local') ASC, id ASC
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
FROM media_item_cards mi
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
FROM media_item_cards mi
WHERE mi.id = ANY(sqlc.arg(ids)::bigint[])
  AND NOT EXISTS (
    SELECT 1 FROM library_files lf
    WHERE lf.media_item_id = mi.id AND lf.deleted_at IS NULL
  );

-- name: SearchMediaItemsByLibrary :many
SELECT * FROM media_item_cards
WHERE library_id = $1
  AND ($4::text = '' OR title ILIKE '%' || $4 || '%')
ORDER BY sort_title ASC, title ASC
LIMIT $2 OFFSET $3;

-- name: UpdateMediaItemPosterPath :exec
WITH profile AS (
  UPDATE media_item_profiles SET poster_path = $2, updated_at = now() WHERE media_item_id = $1
)
UPDATE media_items SET updated_at = now() WHERE id = $1;

-- name: UpdateMediaItemBackdropPath :exec
WITH profile AS (
  UPDATE media_item_profiles SET backdrop_path = $2, updated_at = now() WHERE media_item_id = $1
)
UPDATE media_items SET updated_at = now() WHERE id = $1;

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
FROM media_item_cards mi
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
FROM media_item_cards mi
JOIN tv_series ts ON ts.media_item_id = mi.id
ORDER BY mi.sort_title ASC, mi.title ASC
LIMIT $1 OFFSET $2;
