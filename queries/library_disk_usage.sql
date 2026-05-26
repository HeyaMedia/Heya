-- name: UpsertLibraryDiskUsage :exec
-- Called once per (library, path) at the end of a scan. The scanned_at
-- column re-bumps so the FE can show "last scanned 2 minutes ago" rather
-- than the date of the first-ever scan.
INSERT INTO library_disk_usage (library_id, path, bytes, file_count, scanned_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (library_id, path)
DO UPDATE SET
    bytes      = EXCLUDED.bytes,
    file_count = EXCLUDED.file_count,
    scanned_at = now();

-- name: ListLibraryDiskUsage :many
-- Storage page consumer: every cached reading across every library, sorted
-- newest first so partial state is at least clear (a library scanned once
-- will show its old paths until the next scan completes).
SELECT library_id, path, bytes, file_count, scanned_at
FROM library_disk_usage
ORDER BY library_id, path;

-- name: ListLibraryDiskUsageForLibrary :many
SELECT library_id, path, bytes, file_count, scanned_at
FROM library_disk_usage
WHERE library_id = $1
ORDER BY path;
