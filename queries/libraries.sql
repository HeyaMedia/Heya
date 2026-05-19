-- name: CreateLibrary :one
INSERT INTO libraries (name, media_type, paths, scan_interval, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetLibraryByID :one
SELECT * FROM libraries WHERE id = $1;

-- name: ListLibraries :many
SELECT * FROM libraries ORDER BY created_at ASC;

-- name: UpdateLibrary :one
UPDATE libraries
SET name = $2, paths = $3, scan_interval = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteLibrary :exec
DELETE FROM libraries WHERE id = $1;
