-- name: ListLibraryNFODirs :many
SELECT dir_path, nfo_name, mtime FROM library_nfo_dirs WHERE library_id = $1;

-- name: UpsertLibraryNFODir :exec
INSERT INTO library_nfo_dirs (library_id, dir_path, nfo_name, mtime)
VALUES ($1, $2, $3, $4)
ON CONFLICT (library_id, dir_path) DO UPDATE
SET nfo_name = EXCLUDED.nfo_name, mtime = EXCLUDED.mtime, updated_at = now();

-- name: DeleteLibraryNFODirs :exec
DELETE FROM library_nfo_dirs WHERE library_id = $1 AND dir_path = ANY($2::text[]);
