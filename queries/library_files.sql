-- name: UpsertLibraryFile :one
-- The conflict branch means the bytes changed (or a force rescan), so stale
-- probe artifacts are cleared — ProcessFile skips ffprobe when media_info is
-- already populated, and this reset is what makes that skip safe. NFO-only
-- re-applies must NOT come through here (they'd wipe good probe data); they
-- use ReapplyLibraryFileParse instead.
INSERT INTO library_files (library_id, path, size, mtime, parse_result, status)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (library_id, path) DO UPDATE
SET size = EXCLUDED.size, mtime = EXCLUDED.mtime,
    parse_result = EXCLUDED.parse_result, status = EXCLUDED.status,
    media_info = '{}'::jsonb, keyframes = NULL,
    deleted_at = NULL, updated_at = now()
RETURNING *;

-- name: ReapplyLibraryFileParse :exec
-- Local-metadata re-apply for a file whose bytes did NOT change (its NFO
-- did). Refreshes parse_result and re-drives the match pipeline, but keeps
-- media_info/keyframes so ProcessFile won't re-probe unchanged bytes.
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
UPDATE library_files
SET media_info = $2, updated_at = now()
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

-- name: ListEpisodeFiles :many
SELECT id, size, parse_result FROM library_files
WHERE media_item_id = $1 AND deleted_at IS NULL AND status = 'matched'
ORDER BY path ASC;

-- name: GetMediaItemByExternalID :one
-- Link by provider id. Guarded against the empty-object trap: `external_ids @>
-- '{}'` matches EVERY row, so without the `<> '{}'` filter a stub with no
-- provider IDs would link onto an arbitrary existing media_item. Callers must
-- still skip this for empty IDs; the filter + deterministic ORDER BY are
-- defense in depth so a future caller can't reintroduce the mis-link.
SELECT * FROM media_items
WHERE library_id = $1
  AND sqlc.arg(ext_filter)::jsonb <> '{}'::jsonb
  AND external_ids @> sqlc.arg(ext_filter)::jsonb
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
SELECT lf.media_item_id,
       max(
         COALESCE(
           (SELECT (s->>'height')::int
            FROM jsonb_array_elements(lf.media_info->'streams') AS s
            WHERE s->>'codec_type' = 'video'
            LIMIT 1),
           0
         )
       )::int AS max_height
FROM library_files lf
WHERE lf.media_item_id = ANY(@media_item_ids::bigint[])
  AND lf.deleted_at IS NULL
  AND lf.media_info IS NOT NULL
GROUP BY lf.media_item_id;

-- name: ListUnprobedProbeableFiles :many
-- Files that are already known (not 'pending' — those flow through ProcessFile)
-- but were never successfully probed (media_info still empty). The scan
-- re-enqueues ffprobe for these so a file whose first probe failed (e.g. a
-- flaky mount) isn't stuck unprobed forever. Capped per call; ffprobe jobs are
-- unique-while-active, so repeating this across scans never stacks duplicates.
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
-- metadata_match is unique-while-active so re-drives coalesce across scans.
SELECT * FROM library_files
WHERE library_id = $1
  AND deleted_at IS NULL
  AND status = 'unmatched'
  AND error_message LIKE 'search error:%'
ORDER BY id
LIMIT $2;
