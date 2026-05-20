-- name: UpsertLibraryFile :one
INSERT INTO library_files (library_id, path, size, mtime, parse_result, status)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (library_id, path) DO UPDATE
SET size = EXCLUDED.size, mtime = EXCLUDED.mtime,
    parse_result = EXCLUDED.parse_result, status = EXCLUDED.status,
    deleted_at = NULL, updated_at = now()
RETURNING *;

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
SELECT * FROM library_files WHERE media_item_id = $1 AND deleted_at IS NULL LIMIT 1;

-- name: GetMediaItemByExternalID :one
SELECT * FROM media_items
WHERE library_id = $1 AND external_ids @> $2::jsonb;
