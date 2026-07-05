-- name: ListMediaSegmentsForFile :many
SELECT * FROM media_segments
WHERE library_file_id = $1
ORDER BY start_ms, id;

-- name: DeleteMediaSegmentsForFile :exec
DELETE FROM media_segments WHERE library_file_id = $1;

-- name: InsertMediaSegment :exec
INSERT INTO media_segments (library_file_id, segment_type, start_ms, end_ms, source)
VALUES ($1, $2, $3, $4, $5);

-- name: MarkFileSegmentsAnalyzed :exec
UPDATE library_files SET segments_analyzed_at = now() WHERE id = $1;

-- name: ListFilesPendingSegments :many
-- Playable movie/TV files that have never had a segments pass, plus
-- previously-checked misses old enough to re-check (community databases
-- grow — a miss today is often a hit next week). Requires media_info
-- (the duration gate needs the probed runtime) and at least one external
-- id the segment databases key on. The kickoff pump sweeps this with an
-- id cursor so one run visits each candidate exactly once.
SELECT lf.id, lf.path
FROM library_files lf
JOIN media_items mi ON mi.id = lf.media_item_id
JOIN libraries l ON l.id = lf.library_id
WHERE l.media_type IN ('movie', 'tv')
  AND lf.deleted_at IS NULL
  AND lf.media_info IS NOT NULL
  AND mi.external_ids ?| ARRAY['tmdb', 'imdb', 'tvdb']
  AND lf.id > sqlc.arg(after_id)::bigint
  AND (
    lf.segments_analyzed_at IS NULL
    OR (
      lf.segments_analyzed_at < now() - interval '7 days'
      AND NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id)
    )
  )
ORDER BY lf.id
LIMIT sqlc.arg(row_limit)::int;
