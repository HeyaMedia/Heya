-- name: CreateLibrary :one
INSERT INTO libraries (name, media_type, paths, scan_interval, created_by, settings)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetLibraryByID :one
SELECT * FROM libraries WHERE id = $1;

-- name: GetLibraryByName :one
SELECT * FROM libraries WHERE name = $1;

-- name: UpdateLibraryIdentity :one
UPDATE libraries
SET paths = $2, media_type = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListLibraries :many
SELECT * FROM libraries ORDER BY created_at ASC;

-- name: UpdateLibrary :one
UPDATE libraries
SET name = $2, paths = $3, scan_interval = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateLibrarySettings :one
UPDATE libraries
SET settings = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteLibrary :exec
DELETE FROM libraries WHERE id = $1;
