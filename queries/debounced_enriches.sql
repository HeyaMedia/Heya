-- name: UpsertDebouncedEnrich :exec
-- Push the trailing-edge debounce forward for a media_item. Called from
-- the matcher whenever a new child row (album / track / season / episode)
-- is created under a media_item that's already in enrichment_status =
-- 'complete'. fire_at is always replaced with the new value so repeated
-- calls within the debounce window slide the wall-clock target forward.
INSERT INTO debounced_enriches (media_item_id, fire_at, requested_by)
VALUES (sqlc.arg(media_item_id), sqlc.arg(fire_at), sqlc.arg(requested_by))
ON CONFLICT (media_item_id) DO UPDATE
SET fire_at      = EXCLUDED.fire_at,
    requested_by = EXCLUDED.requested_by;

-- name: LockDueDebouncedEnriches :many
-- Pull every row whose fire_at has elapsed, locking them for the duration
-- of the sweeper's transaction. SKIP LOCKED keeps a slow sweeper from
-- blocking the next tick — concurrent invocations split the workload
-- cleanly instead of serializing. Caller is expected to delete each
-- returned row after handing the work off to River.
SELECT media_item_id, fire_at, requested_by
FROM debounced_enriches
WHERE fire_at <= now()
ORDER BY fire_at ASC
LIMIT sqlc.arg(batch_size)
FOR UPDATE SKIP LOCKED;

-- name: DeleteDebouncedEnrich :exec
DELETE FROM debounced_enriches WHERE media_item_id = sqlc.arg(media_item_id);

-- name: CountDebouncedEnriches :one
SELECT count(*)::int FROM debounced_enriches;
