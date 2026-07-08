-- name: UpsertLibraryFile :one
-- The conflict branch means the bytes changed (or a force rescan), so stale
-- probe artifacts are cleared. NFO-only re-applies must NOT come through here
-- (they'd wipe good probe data); they use ReapplyLibraryFileParse instead.
INSERT INTO library_files (library_id, path, size, mtime, parse_result, status)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (library_id, path) DO UPDATE
SET size = EXCLUDED.size, mtime = EXCLUDED.mtime,
    parse_result = EXCLUDED.parse_result, status = EXCLUDED.status,
    media_info = '{}'::jsonb, keyframes = NULL, video_height = 0,
    deleted_at = NULL, updated_at = now()
RETURNING *;

-- name: ListSeriesWithUnresolvedAbsoluteFiles :many
-- media_item_ids of series still holding a matched absolute-numbered anime file
-- that hasn't been resolved to a real season/episode yet (absoluteEpisodes
-- present, seasons still empty). Drives the one-time startup backfill for series
-- enriched before resolve-and-store existed. Self-limiting: once reconciled a
-- file gains a seasons array and drops out, so a steady-state boot returns
-- nothing. See matcher.ReconcileAbsoluteEpisodes.
-- NULLIF(...,'null') guards the JSON-null case: an absolute file marshals
-- seasons/episodes as `null` (nil slice), and jsonb_array_length('null')
-- errors — COALESCE only catches SQL NULL (absent key), not jsonb null.
SELECT DISTINCT media_item_id
FROM library_files
WHERE status = 'matched'
  AND deleted_at IS NULL
  AND media_item_id IS NOT NULL
  AND jsonb_array_length(COALESCE(NULLIF(parse_result->'parsed'->'release'->'absoluteEpisodes', 'null'::jsonb), '[]'::jsonb)) > 0
  AND jsonb_array_length(COALESCE(NULLIF(parse_result->'parsed'->'release'->'seasons', 'null'::jsonb), '[]'::jsonb)) = 0;

-- name: SetLibraryFileResolvedEpisodes :exec
-- Writes catalog-resolved season/episode arrays into an absolute-numbered anime
-- file's parse_result, in place, without disturbing status/media_item_id/
-- media_info. This is what makes an absolute file ("Series - 24 - Title", parsed
-- with only absoluteEpisodes) look like a normal SxxExx file to every downstream
-- file<->episode join. Idempotent: the reconcile step recomputes from the
-- unchanged absoluteEpisodes each run. See matcher.ReconcileAbsoluteEpisodes.
UPDATE library_files
SET parse_result = jsonb_set(
        jsonb_set(parse_result, '{parsed,release,seasons}', sqlc.arg(seasons)::jsonb, true),
        '{parsed,release,episodes}', sqlc.arg(episodes)::jsonb, true),
    updated_at = now()
WHERE id = sqlc.arg(id)::bigint;

-- name: ReapplyLibraryFileParse :exec
-- Local-metadata re-apply for a file whose bytes did NOT change (its NFO
-- did). Refreshes parse_result and re-drives scanner processing, but keeps
-- media_info/keyframes so unchanged bytes won't be re-probed.
UPDATE library_files
SET parse_result = $2, status = 'pending', error_message = '', updated_at = now()
WHERE id = $1;

-- name: ListLibraryFilesForScan :many
-- One-shot preload of every known file (soft-deleted included — the scanner
-- needs those for the restore path) so the walk does map lookups instead of
-- one SELECT per file. has_nfo says whether local metadata was ever applied,
-- without shipping the whole parse_result across.
SELECT id, path, size, mtime, deleted_at, has_trickplay,
       (parse_result ? 'nfo')::boolean AS has_nfo
FROM library_files
WHERE library_id = $1;

-- name: GetLibraryFileByID :one
SELECT * FROM library_files WHERE id = $1;

-- name: GetLibraryFileByPath :one
SELECT * FROM library_files WHERE library_id = $1 AND path = $2;

-- name: ListLibraryFiles :many
SELECT * FROM library_files
WHERE library_id = $1 AND deleted_at IS NULL
ORDER BY path ASC
LIMIT $2 OFFSET $3;

-- name: ListLibraryFilesByStatus :many
SELECT * FROM library_files
WHERE library_id = $1 AND status = @status AND deleted_at IS NULL
ORDER BY path ASC
LIMIT $2 OFFSET $3;

-- name: UpdateLibraryFileStatus :exec
UPDATE library_files
SET status = $2, media_item_id = $3, error_message = $4, updated_at = now()
WHERE id = $1;

-- name: UpdateLibraryFileMediaInfo :exec
-- video_height is denormalized from the ffprobe payload here — the single
-- writer of media_info (ffprobe worker, service/probe, matcher/music all come
-- through this query), so the derived column can't drift. The browse pages
-- read it via ListMediaResolutions instead of digging through media_info
-- jsonb per row (which cost ~1.5s/page at 71k probed files).
UPDATE library_files
SET media_info = $2,
    video_height = COALESCE(
      (SELECT (s->>'height')::int
       FROM jsonb_array_elements($2::jsonb->'streams') AS s
       WHERE s->>'codec_type' = 'video'
       LIMIT 1), 0),
    updated_at = now()
WHERE id = $1;

-- name: UpdateLibraryFileKeyframes :exec
UPDATE library_files
SET keyframes = $2, updated_at = now()
WHERE id = $1;

-- name: SoftDeleteLibraryFile :exec
UPDATE library_files
SET deleted_at = now(), updated_at = now()
WHERE id = $1;

-- name: SoftDeleteLibraryFilesByPath :exec
UPDATE library_files
SET deleted_at = now(), updated_at = now()
WHERE library_id = $1 AND path = ANY($2::text[]) AND deleted_at IS NULL;

-- name: RestoreLibraryFile :exec
UPDATE library_files
SET deleted_at = NULL, updated_at = now()
WHERE id = $1;

-- name: PurgeDeletedLibraryFiles :exec
DELETE FROM library_files
WHERE library_id = $1 AND deleted_at IS NOT NULL AND deleted_at < $2;

-- name: ListDeletedLibraryFiles :many
SELECT * FROM library_files
WHERE library_id = $1 AND deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT $2 OFFSET $3;

-- name: CountDeletedLibraryFiles :one
SELECT count(*) FROM library_files
WHERE library_id = $1 AND deleted_at IS NOT NULL;

-- name: DeleteLibraryFile :exec
DELETE FROM library_files WHERE id = $1;

-- name: DeleteLibraryFilesByPath :exec
DELETE FROM library_files WHERE library_id = $1 AND path = ANY($2::text[]);

-- name: CountLibraryFilesByStatus :many
SELECT status, count(*) as count
FROM library_files
WHERE library_id = $1 AND deleted_at IS NULL
GROUP BY status;

-- name: ListAllLibraryFilePaths :many
SELECT path FROM library_files WHERE library_id = $1 AND deleted_at IS NULL;

-- name: ListLibraryFilesByMediaItem :many
SELECT * FROM library_files WHERE media_item_id = $1 AND deleted_at IS NULL ORDER BY path ASC;

-- name: ListLibraryFileSizesByMediaItem :many
-- Narrow variant for the media-detail response, which only renders id+size:
-- SELECT * detoasts media_info/parse_result/keyframes jsonb for every file —
-- ~30MB and ~750ms for a big music artist. COLLATE "C" keeps the first row
-- (the FE's playable file) deterministic while skipping the expensive
-- en_US.utf8 collation on long common-prefix paths.
SELECT id, size FROM library_files
WHERE media_item_id = $1 AND deleted_at IS NULL
ORDER BY path COLLATE "C" ASC;

-- name: ListEpisodeFiles :many
SELECT id, size, parse_result FROM library_files
WHERE media_item_id = $1 AND deleted_at IS NULL AND status = 'matched'
ORDER BY path ASC;

-- name: ListEpisodeFileParses :many
-- Slim multi-item variant of ListEpisodeFiles: only the parsed season/episode
-- arrays, not the multi-KB parse_result blob. Feeds presentEpisodeTotals —
-- the show-level watched rollups measure progress against the episodes we
-- actually hold, and pulling full parse_results for every watched show on a
-- browse-state load would be megabytes.
SELECT media_item_id,
       parse_result->'parsed'->'release'->'seasons'  AS seasons,
       parse_result->'parsed'->'release'->'episodes' AS episodes
FROM library_files
WHERE media_item_id = ANY(sqlc.arg(media_item_ids)::bigint[])
  AND deleted_at IS NULL AND status = 'matched';

-- name: GetMediaItemByExternalID :one
-- Link by provider id. Guarded against the empty-object trap: `external_ids @>
-- '{}'` matches EVERY row, so without the `<> '{}'` filter a stub with no
-- provider IDs would link onto an arbitrary existing media_item. Callers must
-- still skip this for empty IDs; the filter + deterministic ORDER BY are
-- defense in depth so a future caller can't reintroduce the mis-link.
SELECT mi.*
FROM media_item_cards mi
WHERE mi.library_id = $1
  AND sqlc.arg(ext_filter)::jsonb <> '{}'::jsonb
  AND NOT EXISTS (
    SELECT 1
    FROM jsonb_each_text(sqlc.arg(ext_filter)::jsonb) AS wanted(provider, external_id)
    WHERE NOT EXISTS (
      SELECT 1
      FROM media_item_external_ids ei
      WHERE ei.media_item_id = mi.id
        AND ei.provider = wanted.provider
        AND ei.external_id = wanted.external_id
    )
  )
ORDER BY id
LIMIT 1;

-- name: UpdateLibraryFileTrickplay :exec
UPDATE library_files
SET has_trickplay = $2, updated_at = now()
WHERE id = $1;

-- name: SetTrickplayByPath :exec
UPDATE library_files
SET has_trickplay = $2, updated_at = now()
WHERE library_id = $1 AND path = $3 AND deleted_at IS NULL;

-- name: UpdateLibraryFileContentHash :exec
UPDATE library_files
SET content_hash = $2, updated_at = now()
WHERE id = $1;

-- name: ListDeletedFilesBySize :many
-- Move-detection candidates: recently soft-deleted files with the same byte
-- size. Size alone is NOT sufficient to claim a move — the scanner requires a
-- matching basename or mtime on top (see relocate logic in scanner.go) so a
-- coincidentally same-sized new file can't inherit a deleted file's
-- identity/watch history. Newest deletions first for deterministic preference.
SELECT * FROM library_files
WHERE library_id = $1 AND size = $2 AND deleted_at IS NOT NULL
  AND deleted_at > now() - interval '7 days'
ORDER BY deleted_at DESC
LIMIT 16;

-- name: RelocateLibraryFile :exec
UPDATE library_files
SET path = $2, mtime = $3, parse_result = $4, deleted_at = NULL, updated_at = now()
WHERE id = $1;

-- name: ListMediaResolutions :many
-- Reads the denormalized video_height (written by UpdateLibraryFileMediaInfo,
-- backfilled by migration 00037) instead of unpacking media_info jsonb per
-- row: the jsonb variant seq-scanned 673k rows and detoasted 71k ffprobe
-- payloads per TV browse (~1.5s). Served index-only by
-- idx_library_files_media_item_height. Keep the ::int cast — it pins the
-- sqlc row shape to MaxHeight int32.
SELECT media_item_id,
       max(video_height)::int AS max_height
FROM library_files
WHERE media_item_id = ANY(@media_item_ids::bigint[])
  AND deleted_at IS NULL
GROUP BY media_item_id;

-- name: ListUnprobedProbeableFiles :many
-- Files that are already known but were never successfully probed (media_info
-- still empty). The scan re-enqueues ffprobe for these so a file whose first
-- probe failed (e.g. a flaky mount) isn't stuck unprobed forever. Capped per
-- call; ffprobe jobs are unique-while-active, so repeating this across scans
-- never stacks duplicates.
SELECT * FROM library_files
WHERE library_id = $1
  AND deleted_at IS NULL
  AND status <> 'pending'
  AND (media_info = '{}'::jsonb OR media_info = 'null'::jsonb)
ORDER BY id
LIMIT $2;

-- name: ListRetryableUnmatchedFiles :many
-- Files stranded as 'unmatched' by a TRANSIENT provider search error (the
-- matcher records "search error: ..." there). A genuine "no results" / "no
-- title" match is NOT retried (different error_message), so this only re-drives
-- files that could plausibly match once the provider recovers. Capped;
-- scanner runs are unique-while-active so re-drives coalesce across scans.
SELECT * FROM library_files
WHERE library_id = $1
  AND deleted_at IS NULL
  AND status = 'unmatched'
  AND error_message LIKE 'search error:%'
ORDER BY id
LIMIT $2;
