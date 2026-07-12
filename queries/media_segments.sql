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

-- Precedence, final: manual beats everything. Community and chromaprint are
-- peers by arrival order — whichever writes first for a given (file,
-- segment_type) wins, and neither is allowed to clobber the other.
-- blackframe (heuristic fallback) loses to both and to manual.
--
-- This replaced an earlier policy where chromaprint (a direct per-file
-- measurement) unconditionally outranked and replaced community data,
-- because TheIntroDB carries no authored runtime and a bad match could
-- still slip past the duration gate. In practice, a real incident that
-- looked exactly like that — chromaprint disagreeing with a community
-- marker — turned out to be a constant offset in the web player's own
-- clock, not bad community data; the community marker was right all
-- along. Local detection is now a gap-filler: it only computes what the
-- community pass couldn't answer, and never second-guesses a community
-- row (or vice versa) once one exists.

-- name: ExistsManualMediaSegment :one
-- Precedence: manual beats everything. Checked before inserting a picked
-- winner of the same type — a user-authored correction is never
-- overwritten by community data or a chromaprint measurement.
SELECT EXISTS(
    SELECT 1 FROM media_segments
    WHERE library_file_id = $1 AND segment_type = $2 AND source = 'manual'
) AS exists;

-- name: ExistsChromaprintMediaSegment :one
-- Precedence: community and chromaprint are peers by arrival order for a
-- given (file, type) — whichever gets there first wins. The community
-- worker checks this before inserting a picked winner of the same type so
-- a fresh community fetch never overwrites an existing chromaprint
-- measurement (see the top-of-file precedence note for why chromaprint no
-- longer unconditionally outranks community).
SELECT EXISTS(
    SELECT 1 FROM media_segments
    WHERE library_file_id = $1 AND segment_type = $2 AND source = 'chromaprint'
) AS exists;

-- name: ExistsCommunityMediaSegmentForFileAndType :one
-- Mirror image of ExistsChromaprintMediaSegment: the local chromaprint
-- detector checks this before inserting a measured winner so it never
-- overwrites a community row that got there first — community and
-- chromaprint are peers by arrival order, not a strict ranking (see the
-- top-of-file precedence note).
SELECT EXISTS(
    SELECT 1 FROM media_segments
    WHERE library_file_id = $1 AND segment_type = $2 AND source LIKE 'community:%'
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

-- name: UpsertMediaSegmentByRank :exec
-- Precedence-aware upsert — the single-statement source of truth for
-- every non-commercial segment write. The community worker and the local
-- detection workers run concurrently on different queues behind
-- read-committed EXISTS guards, so any two writers can both pass their
-- checks and race the insert (a plain ON CONFLICT DO NOTHING would keep
-- whichever row COMMITTED first, letting a blackframe guess permanently
-- beat a chromaprint measurement that lost the commit race). The partial
-- unique index idx_media_segments_file_type makes the second write
-- conflict, and the rank comparison resolves every ordering in place:
--
--   manual (2)  >  chromaprint == community:% (1)  >  blackframe (0)
--
-- A strictly weaker existing row is overwritten (chromaprint or
-- community landing after a blackframe guess replaces it in place). An
-- equal or stronger existing row makes the write a no-op: community and
-- chromaprint are peers by arrival order, so whichever committed first
-- wins and the loser vanishes silently — the intended policy, not an
-- error. (Deliberate deviation from ranking chromaprint above community
-- here: doing so would let a racing measurement overwrite a committed
-- community row, which is exactly the replace-on-sight behavior the
-- gap-filler revert removed.)
--
-- Equal-rank corollary: a community re-fetch can NOT update its own rows
-- through this upsert (community-over-community no-ops). The weekly
-- refresh relies on writeCommunitySegments deleting the file's
-- community:% rows in the same transaction BEFORE re-inserting, so its
-- inserts never meet their own old rows.
--
-- The conflict target is the partial index, so commercial rows never
-- conflict here — the workers write commercials through the plain
-- InsertMediaSegment (multiple breaks per file are legitimate).
INSERT INTO media_segments (library_file_id, segment_type, start_ms, end_ms, source)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (library_file_id, segment_type) WHERE segment_type <> 'commercial'
DO UPDATE SET
    start_ms = EXCLUDED.start_ms,
    end_ms = EXCLUDED.end_ms,
    source = EXCLUDED.source,
    created_at = now()
WHERE (CASE WHEN media_segments.source = 'manual' THEN 2
            WHEN media_segments.source = 'chromaprint' THEN 1
            WHEN media_segments.source LIKE 'community:%' THEN 1
            ELSE 0 END)
    < (CASE WHEN EXCLUDED.source = 'manual' THEN 2
            WHEN EXCLUDED.source = 'chromaprint' THEN 1
            WHEN EXCLUDED.source LIKE 'community:%' THEN 1
            ELSE 0 END);

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
JOIN media_item_cards mi ON mi.id = lf.media_item_id
JOIN libraries l ON l.id = lf.library_id
WHERE l.media_type IN ('movie', 'tv', 'anime')
  AND lf.deleted_at IS NULL
  AND lf.media_info IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM media_item_external_ids ei
    WHERE ei.media_item_id = mi.id
      AND ei.provider IN ('tmdb', 'imdb', 'tvdb')
  )
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
-- Distinct (series, season) pairs with at least one PENDING episode file
-- and at least two eligible files overall. A file is pending when the
-- community pass already ran (segments_analyzed_at NOT NULL), local
-- detection hasn't stamped it (segments_detected_at IS NULL), and it's
-- still missing an intro or credits row (any source) — the gap local
-- detection exists to fill. A file the community pass fully covered is
-- never pending (gap-filler policy, not a re-measurement pass).
--
-- The >= 2 floor counts ALL eligible files, not just pending ones:
-- cross-episode matching needs a partner to compare against, but the
-- partner does NOT need to be pending — a community-covered episode's
-- audio is perfectly good comparison material. An earlier version had
-- HAVING count(pending) >= 2, which stranded exactly the lone-gap shape
-- (community covered 12 of 13 episodes; the one hole could never run).
-- A single-file season with a gap still never qualifies — there is
-- nothing to pair against — and staying unlisted keeps the pump from
-- looping on it.
--
-- Cursor: season numbers are always well under 100000 (no real season
-- count comes close), so `media_item_id * 100000 + season` packs both
-- into one monotonic bigint cursor key without a composite-cursor WHERE
-- clause — exposed as cursor_key below, ordered and filtered on directly.
WITH eligible AS (
    SELECT
        lf.media_item_id AS media_item_id,
        (lf.parse_result->'parsed'->'release'->'seasons'->>0)::int AS season,
        (
            lf.segments_detected_at IS NULL
            AND (
                NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'intro')
                OR NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'credits')
            )
        ) AS pending
    FROM library_files lf
    JOIN libraries l ON l.id = lf.library_id
    WHERE l.media_type IN ('tv', 'anime')
      AND lf.deleted_at IS NULL
      AND lf.media_info IS NOT NULL
      AND lf.media_item_id IS NOT NULL
      AND lf.segments_analyzed_at IS NOT NULL
      AND (lf.parse_result->'parsed'->'release'->'seasons'->>0) IS NOT NULL
)
SELECT
    media_item_id,
    season,
    (count(*) FILTER (WHERE pending))::int AS pending_files,
    (media_item_id * 100000 + season)::bigint AS cursor_key
FROM eligible
GROUP BY media_item_id, season
HAVING count(*) FILTER (WHERE pending) >= 1
   AND count(*) >= 2
   AND (media_item_id * 100000 + season) > sqlc.arg(after_key)::bigint
ORDER BY (media_item_id * 100000 + season)
LIMIT sqlc.arg(row_limit)::int;

-- name: ListEpisodeFilesForSeasonDetection :many
-- ALL eligible episode files for one (media_item_id, season) pair — the
-- pending gap-fill targets AND the already-covered partners. Cross-episode
-- matching needs a partner to compare against, but the partner does NOT
-- need to be pending: a community-covered episode's audio is perfectly
-- good comparison material (the lone-gap season — community covered 12 of
-- 13 episodes — must still be able to resolve its one hole).
--
-- pending marks the actual targets (same condition as
-- ListSeasonsPendingDetection's CTE): only pending files get segments
-- written and segments_detected_at stamped. Partners are never written or
-- stamped, and are only fingerprinted when a nearby target actually needs
-- the comparison (see resolveRegionsForTargets). has_intro/has_credits
-- report whether ANY row (any source) already covers that type — the
-- worker derives each pending file's missing types from them, so a window
-- nobody needs is never decoded. The per-file, per-type write guard is
-- enforced separately at write time by insertChromaprintSegmentIfAbsent.
-- Ordered by episode number so nearest-neighbor pairing tries adjacent
-- episodes first.
SELECT
    lf.id,
    lf.path,
    lf.media_info,
    (lf.parse_result->'parsed'->'release'->'episodes'->>0)::int AS episode_number,
    EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'intro') AS has_intro,
    EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'credits') AS has_credits,
    (
        lf.segments_detected_at IS NULL
        AND (
            NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'intro')
            OR NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'credits')
        )
    ) AS pending
FROM library_files lf
JOIN libraries l ON l.id = lf.library_id
WHERE l.media_type IN ('tv', 'anime')
  AND lf.deleted_at IS NULL
  AND lf.media_info IS NOT NULL
  AND lf.media_item_id = sqlc.arg(media_item_id)::bigint
  AND (lf.parse_result->'parsed'->'release'->'seasons'->>0)::int = sqlc.arg(season)::int
  AND lf.segments_analyzed_at IS NOT NULL
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
