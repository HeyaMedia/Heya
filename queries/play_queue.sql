-- Server-owned play queue (docs/queue-plan.md). One queue per user,
-- fully materialized; clients read windows around the pointer. Every
-- structural mutation bumps play_queues.version inside the service tx.
--
-- Ordering keys are sparse (gap 1024). Rewrites (renumber, reshuffle,
-- unshuffle) land in a fresh range ABOVE max(ord) so the (queue_id, ord)
-- unique constraint never sees a transient collision mid-UPDATE.

-- name: EnsurePlayQueue :one
INSERT INTO play_queues (user_id)
VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING *;

-- name: GetPlayQueueByUser :one
SELECT * FROM play_queues WHERE user_id = $1;

-- name: SetQueuePointer :one
UPDATE play_queues
SET current_item_id  = sqlc.narg(current_item_id),
    position_seconds = sqlc.arg(position_seconds),
    playing          = sqlc.arg(playing),
    version = version + 1, updated_at = now()
WHERE id = sqlc.arg(queue_id)
RETURNING *;

-- Heartbeat: coarse renderer position. Deliberately NO version bump —
-- clients don't refetch windows for transport ticks.
-- name: SetQueueTransport :one
UPDATE play_queues
SET position_seconds = sqlc.arg(position_seconds),
    playing          = sqlc.arg(playing),
    updated_at = now()
WHERE id = sqlc.arg(queue_id)
RETURNING *;

-- name: SetQueueModes :one
UPDATE play_queues
SET repeat_mode = sqlc.arg(repeat_mode),
    shuffled    = sqlc.arg(shuffled),
    version = version + 1, updated_at = now()
WHERE id = sqlc.arg(queue_id)
RETURNING *;

-- name: SetQueueOutput :one
UPDATE play_queues
SET active_output = sqlc.arg(active_output),
    version = version + 1, updated_at = now()
WHERE id = sqlc.arg(queue_id)
RETURNING *;

-- After a re-materialization: new source/pointer, position reset.
-- name: SetQueueReplaced :one
UPDATE play_queues
SET source           = sqlc.arg(source),
    shuffled         = sqlc.arg(shuffled),
    current_item_id  = sqlc.narg(current_item_id),
    position_seconds = 0,
    playing          = sqlc.arg(playing),
    version = version + 1, updated_at = now()
WHERE id = sqlc.arg(queue_id)
RETURNING *;

-- name: BumpQueueVersion :one
UPDATE play_queues
SET version = version + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAllQueueItems :exec
DELETE FROM play_queue_items WHERE queue_id = $1;

-- name: DeleteUpcomingQueueItems :execrows
DELETE FROM play_queue_items WHERE queue_id = $1 AND ord > $2;

-- name: DeleteQueueItem :execrows
DELETE FROM play_queue_items WHERE id = $1 AND queue_id = $2;

-- ── Materializers ────────────────────────────────────────────────────
-- Shape shared by all sources: rank the source's natural order into
-- src_ord, pick rows (randomly when shuffling — the LIMIT must apply
-- AFTER the random order so a capped selection is a true random sample),
-- then lay down sparse ords. Only playable tracks (a live file exists)
-- enter the queue.

-- name: InsertQueueItemsFromAlbum :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       1024 * row_number() OVER (ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, x.natural_rank),
       x.id,
       x.natural_rank
FROM (
    SELECT t.id,
           (row_number() OVER (ORDER BY t.disc_number, t.track_number))::int AS natural_rank
    FROM tracks t
    WHERE t.album_id = sqlc.arg(album_id)
      AND (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL))
    ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, 2
    LIMIT sqlc.arg(max_items)
) x;

-- name: InsertQueueItemsFromArtist :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       1024 * row_number() OVER (ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, x.natural_rank),
       x.id,
       x.natural_rank
FROM (
    SELECT t.id,
           (row_number() OVER (ORDER BY al.year, al.id, t.disc_number, t.track_number))::int AS natural_rank
    FROM tracks t
    JOIN albums al ON al.id = t.album_id
    WHERE al.artist_id = sqlc.arg(artist_id)
      AND (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL))
    ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, 2
    LIMIT sqlc.arg(max_items)
) x;

-- name: InsertQueueItemsFromPlaylist :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       1024 * row_number() OVER (ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, x.natural_rank),
       x.id,
       x.natural_rank
FROM (
    SELECT t.id,
           (row_number() OVER (ORDER BY pt.position))::int AS natural_rank
    FROM user_playlist_tracks pt
    JOIN tracks t ON t.id = pt.track_id
    WHERE pt.playlist_id = sqlc.arg(playlist_id)
      AND (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL))
    ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, 2
    LIMIT sqlc.arg(max_items)
) x;

-- Music genre lives on albums (text[]).
-- name: InsertQueueItemsFromGenre :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       1024 * row_number() OVER (ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, x.natural_rank),
       x.id,
       x.natural_rank
FROM (
    SELECT t.id,
           (row_number() OVER (ORDER BY al.artist_id, al.year, al.id, t.disc_number, t.track_number))::int AS natural_rank
    FROM tracks t
    JOIN albums al ON al.id = t.album_id
    WHERE sqlc.arg(genre)::text ILIKE ANY(al.genres)
      AND (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL))
    ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, 2
    LIMIT sqlc.arg(max_items)
) x;

-- Explicit track list (mixes, multi-select, FE-assembled contexts).
-- Natural order = the order given.
-- name: InsertQueueItemsFromTrackIDs :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       1024 * row_number() OVER (ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, x.natural_rank),
       x.id,
       x.natural_rank
FROM (
    SELECT t.id, u.ordinality::int AS natural_rank
    FROM unnest(sqlc.arg(track_ids)::bigint[]) WITH ORDINALITY AS u(track_id, ordinality)
    JOIN tracks t ON t.id = u.track_id
    WHERE (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL))
    ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, 2
    LIMIT sqlc.arg(max_items)
) x;

-- Whole music library ("surprise me").
-- name: InsertQueueItemsFromLibrary :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       1024 * row_number() OVER (ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, x.natural_rank),
       x.id,
       x.natural_rank
FROM (
    SELECT t.id,
           (row_number() OVER (ORDER BY t.id))::int AS natural_rank
    FROM tracks t
    WHERE (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL))
    ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, 2
    LIMIT sqlc.arg(max_items)
) x;

-- Append/insert an explicit list at service-computed ords: ord = base +
-- step*n, src_ord continues from base_src. The service guarantees the
-- target gap fits (renumbering first if not).
-- name: InsertQueueItemsAt :execrows
INSERT INTO play_queue_items (queue_id, ord, track_id, src_ord)
SELECT sqlc.arg(queue_id),
       sqlc.arg(base_ord)::bigint + sqlc.arg(step)::bigint * u.ordinality,
       t.id,
       sqlc.arg(base_src)::int + u.ordinality::int
FROM unnest(sqlc.arg(track_ids)::bigint[]) WITH ORDINALITY AS u(track_id, ordinality)
JOIN tracks t ON t.id = u.track_id
WHERE (EXISTS (SELECT 1 FROM track_files tf JOIN library_files lf ON lf.id = tf.library_file_id WHERE tf.track_id = t.id AND lf.deleted_at IS NULL)
           OR EXISTS (SELECT 1 FROM library_files lf2 WHERE lf2.id = t.library_file_id AND lf2.deleted_at IS NULL));

-- Rewrite the tail after after_ord into a fresh range above max(ord):
-- shuffle=true orders randomly, shuffle=false restores the source's
-- natural order (src_ord). Both are the same one-statement rewrite.
-- name: ReorderUpcoming :exec
UPDATE play_queue_items p
SET ord = sub.new_ord
FROM (
    SELECT it.id,
           (SELECT COALESCE(MAX(mx.ord), 0) FROM play_queue_items mx WHERE mx.queue_id = sqlc.arg(queue_id))
           + 1024 * row_number() OVER (
               ORDER BY CASE WHEN sqlc.arg(shuffle)::boolean THEN random() END NULLS LAST, it.src_ord, it.id
             ) AS new_ord
    FROM play_queue_items it
    WHERE it.queue_id = sqlc.arg(queue_id) AND it.ord > sqlc.arg(after_ord)
) sub
WHERE p.id = sub.id;

-- Full renumber preserving current order — run when a move/insert finds
-- no usable gap.
-- name: RenumberQueueItems :exec
UPDATE play_queue_items p
SET ord = sub.new_ord
FROM (
    SELECT it.id,
           (SELECT COALESCE(MAX(mx.ord), 0) FROM play_queue_items mx WHERE mx.queue_id = sqlc.arg(queue_id))
           + 1024 * row_number() OVER (ORDER BY it.ord, it.id) AS new_ord
    FROM play_queue_items it
    WHERE it.queue_id = sqlc.arg(queue_id)
) sub
WHERE p.id = sub.id;

-- name: GetQueueItem :one
SELECT * FROM play_queue_items WHERE id = $1 AND queue_id = $2;

-- name: FirstQueueItem :one
SELECT * FROM play_queue_items WHERE queue_id = $1 ORDER BY ord LIMIT 1;

-- name: NextQueueItem :one
SELECT * FROM play_queue_items WHERE queue_id = $1 AND ord > $2 ORDER BY ord LIMIT 1;

-- name: PrevQueueItem :one
SELECT * FROM play_queue_items WHERE queue_id = $1 AND ord < $2 ORDER BY ord DESC LIMIT 1;

-- name: FindQueueItemByTrack :one
SELECT * FROM play_queue_items WHERE queue_id = $1 AND track_id = $2 ORDER BY ord LIMIT 1;

-- name: CountQueueItems :one
SELECT count(*) FROM play_queue_items WHERE queue_id = $1;

-- name: CountQueueItemsBefore :one
SELECT count(*) FROM play_queue_items WHERE queue_id = $1 AND ord < $2;

-- name: MaxQueueOrd :one
SELECT COALESCE(MAX(ord), 0)::bigint AS max_ord FROM play_queue_items WHERE queue_id = $1;

-- name: MaxQueueSrcOrd :one
SELECT COALESCE(MAX(src_ord), 0)::int AS max_src_ord FROM play_queue_items WHERE queue_id = $1;

-- Dedupe helper for enqueue ("don't re-add what's already coming up").
-- name: ListUpcomingQueueTrackIDs :many
SELECT track_id FROM play_queue_items WHERE queue_id = $1 AND ord > $2;

-- name: SetQueueItemOrd :exec
UPDATE play_queue_items SET ord = $3 WHERE id = $1 AND queue_id = $2;

-- History prune support: the ord of the item `keep` positions before
-- current — everything at or below it gets deleted.
-- name: QueueHistoryCutoff :one
SELECT ord FROM play_queue_items
WHERE queue_id = $1 AND ord < $2
ORDER BY ord DESC
OFFSET $3 LIMIT 1;

-- name: DeleteQueueItemsThrough :execrows
DELETE FROM play_queue_items WHERE queue_id = $1 AND ord <= $2;

-- Window reads with full display context (the FE Track shape: title,
-- artist, album, slugs — poster derives from the slugs client-side).
-- name: ListQueueWindow :many
SELECT qi.id, qi.ord, qi.track_id, qi.src_ord,
       t.title, t.duration, t.disc_number, t.track_number,
       al.id  AS album_id, al.title AS album_title, al.slug AS album_slug,
       a.id   AS artist_id, a.name  AS artist_name, mi.slug AS artist_slug
FROM play_queue_items qi
JOIN tracks t   ON t.id  = qi.track_id
JOIN albums al  ON al.id = t.album_id
JOIN artists a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE qi.queue_id = $1 AND qi.ord >= $2
ORDER BY qi.ord
LIMIT $3;

-- name: ListQueueWindowBefore :many
SELECT qi.id, qi.ord, qi.track_id, qi.src_ord,
       t.title, t.duration, t.disc_number, t.track_number,
       al.id  AS album_id, al.title AS album_title, al.slug AS album_slug,
       a.id   AS artist_id, a.name  AS artist_name, mi.slug AS artist_slug
FROM play_queue_items qi
JOIN tracks t   ON t.id  = qi.track_id
JOIN albums al  ON al.id = t.album_id
JOIN artists a  ON a.id  = al.artist_id
JOIN media_item_cards mi ON mi.id = a.media_item_id
WHERE qi.queue_id = $1 AND qi.ord < $2
ORDER BY qi.ord DESC
LIMIT $3;
