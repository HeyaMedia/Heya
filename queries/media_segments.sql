-- name: ListMediaSegmentsForFile :many
SELECT * FROM media_segments
WHERE library_file_id = $1
ORDER BY start_ms, id;

-- name: DeleteMediaSegmentsForFile :exec
DELETE FROM media_segments WHERE library_file_id = $1;

-- name: DeleteCommunityMediaSegmentsForFile :exec
-- Precedence-scoped delete for the community worker's refresh pass: only
-- clears community: rows, leaving any manual row and any local
-- chromaprint/blackframe row untouched. The community worker used to call
-- DeleteMediaSegmentsForFile (all sources) before every re-insert, which
-- would wipe out local-detection results on the file's next weekly
-- re-check even when the community databases still had nothing new.
DELETE FROM media_segments WHERE library_file_id = $1 AND source LIKE 'community:%';

-- Precedence, final: manual > chromaprint (measured on this exact file) >
-- community:* (crowdsourced, duration-gated but not release-verified) >
-- blackframe (heuristic fallback). A real-world false positive is why
-- chromaprint outranks community: TheIntroDB carries no authored runtime
-- (so the duration gate can't verify the release cut), and a bad match can
-- still pass the gate — but chromaprint measures the actual file, so it's
-- always right when it finds a region at all.

-- name: DeleteReplaceableMediaSegmentsForFileAndType :exec
-- Called by the season worker right before it inserts a freshly-measured
-- chromaprint winner of this type: clears any community or blackframe row
-- (both rank below a direct measurement) plus any stale chromaprint row of
-- its own from a prior partial run (retry-safety — this worker's jobs are
-- MaxAttempts 2). A manual row is never touched; ExistsManualMediaSegment
-- gates the whole call.
DELETE FROM media_segments
WHERE library_file_id = $1
  AND segment_type = $2
  AND (source IN ('chromaprint', 'blackframe') OR source LIKE 'community:%');

-- name: DeleteBlackframeMediaSegmentsForFileAndType :exec
-- Called by the community worker right before it inserts a picked winner
-- of this type: a blackframe heuristic guess ranks below community data,
-- so community may replace it (chromaprint measurements are never touched
-- here — see ExistsChromaprintMediaSegment).
DELETE FROM media_segments
WHERE library_file_id = $1 AND segment_type = $2 AND source = 'blackframe';

-- name: ExistsManualMediaSegment :one
-- Precedence: manual beats everything. Checked before inserting a picked
-- winner of the same type — a user-authored correction is never
-- overwritten by community data or a chromaprint measurement.
SELECT EXISTS(
    SELECT 1 FROM media_segments
    WHERE library_file_id = $1 AND segment_type = $2 AND source = 'manual'
) AS exists;

-- name: ExistsChromaprintMediaSegment :one
-- Precedence: chromaprint (measured on this exact file) beats community
-- (crowdsourced, duration-gated but not release-verified). The community
-- worker checks this before inserting a picked winner of the same type —
-- a fresh community fetch never overwrites an existing measurement.
SELECT EXISTS(
    SELECT 1 FROM media_segments
    WHERE library_file_id = $1 AND segment_type = $2 AND source = 'chromaprint'
) AS exists;

-- name: ExistsMediaSegmentForFileAndType :one
-- Insert guard for the movie (blackframe) detection worker: a black-frame
-- heuristic only ever fills a gap, regardless of which source would have
-- won — if any row of this type already exists (manual, community, or
-- chromaprint), blackframe detection leaves it alone.
SELECT EXISTS(
    SELECT 1 FROM media_segments WHERE library_file_id = $1 AND segment_type = $2
) AS exists;

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
      -- Skip the re-check once the file has either a community row or a
      -- chromaprint measurement — chromaprint outranks community (it's
      -- measured on this exact release, community is crowdsourced and
      -- only duration-gated), so once it's filled a type there's no
      -- reason to keep polling the community for it. A blackframe-only
      -- file (movie credits, no chromaprint attempted) still re-checks.
      AND NOT EXISTS (
          SELECT 1 FROM media_segments ms
          WHERE ms.library_file_id = lf.id AND (ms.source LIKE 'community:%' OR ms.source = 'chromaprint')
      )
    )
  )
ORDER BY lf.id
LIMIT sqlc.arg(row_limit)::int;

-- name: MarkFileSegmentsDetected :exec
-- Stamped by the local-detection workers after an attempt, regardless of
-- whether a usable region was found — mirrors segments_analyzed_at /
-- boundaries_analyzed_at: NULL means "not yet attempted", not "nothing to
-- skip", so a permanently-unpairable episode or an undecodable movie tail
-- doesn't get re-fingerprinted on every pump sweep.
UPDATE library_files SET segments_detected_at = now() WHERE id = ANY(sqlc.arg(ids)::bigint[]);

-- name: ListSeasonsPendingDetection :many
-- Distinct (series, season) pairs with at least two TV episode files
-- pending local detection — cross-episode matching needs a pair to
-- compare against, hence HAVING count(*) >= 2. A file is pending purely on
-- segments_analyzed_at NOT NULL AND segments_detected_at IS NULL —
-- deliberately NOT gated on the absence of existing intro/credits rows.
-- Chromaprint (a direct measurement of this file) outranks community data
-- (crowdsourced, duration-gated but not release-verified), so it runs as a
-- background pass over every analyzed episode regardless of whether the
-- community fetch already left a row; the per-file write
-- (replaceWithChromaprintSegment) is the precedence enforcement, replacing
-- community/blackframe rows of the same type (but never a manual one).
--
-- Cursor: season numbers are always well under 100000 (no real season
-- count comes close), so `media_item_id * 100000 + season` packs both
-- into one monotonic bigint cursor key without a composite-cursor WHERE
-- clause — exposed as cursor_key below, ordered and filtered on directly.
WITH pending AS (
    SELECT
        lf.media_item_id AS media_item_id,
        (lf.parse_result->'parsed'->'release'->'seasons'->>0)::int AS season
    FROM library_files lf
    JOIN libraries l ON l.id = lf.library_id
    WHERE l.media_type = 'tv'
      AND lf.deleted_at IS NULL
      AND lf.media_info IS NOT NULL
      AND lf.media_item_id IS NOT NULL
      AND lf.segments_analyzed_at IS NOT NULL
      AND lf.segments_detected_at IS NULL
      AND (lf.parse_result->'parsed'->'release'->'seasons'->>0) IS NOT NULL
)
SELECT
    media_item_id,
    season,
    count(*)::int AS pending_files,
    (media_item_id * 100000 + season)::bigint AS cursor_key
FROM pending
GROUP BY media_item_id, season
HAVING count(*) >= 2
   AND (media_item_id * 100000 + season) > sqlc.arg(after_key)::bigint
ORDER BY (media_item_id * 100000 + season)
LIMIT sqlc.arg(row_limit)::int;

-- name: ListEpisodeFilesForSeasonDetection :many
-- Pending-detection episode files for one (media_item_id, season) pair,
-- ordered by episode number so the season worker's nearest-neighbor
-- pairing tries adjacent episodes first.
SELECT
    lf.id,
    lf.path,
    lf.media_info,
    (lf.parse_result->'parsed'->'release'->'episodes'->>0)::int AS episode_number
FROM library_files lf
JOIN libraries l ON l.id = lf.library_id
WHERE l.media_type = 'tv'
  AND lf.deleted_at IS NULL
  AND lf.media_info IS NOT NULL
  AND lf.media_item_id = sqlc.arg(media_item_id)::bigint
  AND (lf.parse_result->'parsed'->'release'->'seasons'->>0)::int = sqlc.arg(season)::int
  AND lf.segments_analyzed_at IS NOT NULL
  AND lf.segments_detected_at IS NULL
  AND (lf.parse_result->'parsed'->'release'->'episodes'->>0) IS NOT NULL
ORDER BY episode_number;

-- name: ListMovieFilesPendingDetection :many
-- Movie files pending local credits detection: community pass already ran,
-- local detection hasn't, and no credits row exists yet from any source
-- (unlike the season query, movies have only one segment type to detect,
-- so the absence check is cheap to fold into the listing itself). The
-- kickoff pump sweeps this with an id cursor so one run visits each
-- candidate exactly once.
SELECT lf.id, lf.path, lf.media_info
FROM library_files lf
JOIN libraries l ON l.id = lf.library_id
WHERE l.media_type = 'movie'
  AND lf.deleted_at IS NULL
  AND lf.media_info IS NOT NULL
  AND lf.media_item_id IS NOT NULL
  AND lf.segments_analyzed_at IS NOT NULL
  AND lf.segments_detected_at IS NULL
  AND lf.id > sqlc.arg(after_id)::bigint
  AND NOT EXISTS (
      SELECT 1 FROM media_segments ms
      WHERE ms.library_file_id = lf.id AND ms.segment_type = 'credits'
  )
ORDER BY lf.id
LIMIT sqlc.arg(row_limit)::int;
