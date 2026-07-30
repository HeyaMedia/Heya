-- Shadow acquisition pipeline: runs, releases, candidates, decisions.

-- name: InsertManagerPolicySnapshot :exec
INSERT INTO manager_policy_snapshots (policy_hash, snapshot)
VALUES ($1, $2)
ON CONFLICT (policy_hash) DO NOTHING;

-- name: GetManagerPolicySnapshot :one
SELECT * FROM manager_policy_snapshots WHERE policy_hash = $1;

-- name: CreateManagerRun :one
INSERT INTO manager_runs (kind, source, scope)
VALUES ($1, $2, $3)
RETURNING *;

-- name: FinishManagerRun :one
UPDATE manager_runs
SET status = $2, partial = $3, truncated = $4, stats = $5, errors = $6,
    finished_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateManagerRunIndexer :one
INSERT INTO manager_run_indexers (run_id, indexer_id, indexer_name, domain, status, pages_fetched, fetched, duration_ms, error)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateManagerRunRequest :one
INSERT INTO manager_run_requests (run_indexer_id, ordinal, params, page_offset, results, duration_ms, error)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- Release identity is durable: re-seeing a release refreshes the aggregate
-- window and the mutable presentation fields, never reminting the row.
-- name: UpsertManagerRelease :one
INSERT INTO manager_releases (indexer_id, indexer_name, domain, release_key, ui_fingerprint, guid, title, size_bytes, publish_date, publish_date_raw, categories, raw_attrs, info_url)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (indexer_id, domain, release_key) DO UPDATE
SET last_seen_at = now(), times_seen = manager_releases.times_seen + 1,
    title = EXCLUDED.title, size_bytes = EXCLUDED.size_bytes,
    publish_date = COALESCE(EXCLUDED.publish_date, manager_releases.publish_date),
    info_url = EXCLUDED.info_url
RETURNING *;

-- name: CreateManagerReleaseSighting :one
INSERT INTO manager_release_sightings (release_id, run_id, run_request_id, response_attrs, status, policy_hash)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateManagerReleaseSighting :exec
UPDATE manager_release_sightings
SET status = $2, attempts = attempts + 1, error = $3, matched = $4,
    decision_id = $5, policy_hash = $6
WHERE id = $1;

-- name: CreateManagerCandidate :one
INSERT INTO manager_candidates (run_id, release_id, indexer_id, indexer_name, indexer_priority, title, size_bytes, publish_date, categories, parsed, quality, quality_position, format_score, format_breakdown, rejections)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: CreateManagerDecision :one
INSERT INTO manager_decisions (
    run_id, target_kind, target_key, media_item_id, tv_episode_id, music_target_id,
    library_id, domain, target_title, target_year,
    season_number, episode_number, absolute_number,
    artist_name, album_type, edition_key, album_title,
    profile_id, profile_name, policy_hash, evaluator_version, parser_version,
    verdict, context
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
RETURNING *;

-- would_grab decisions insert with a provisional verdict (the chosen⇔grab
-- CHECK can't defer, unlike the FK) and flip to their real verdict in one
-- atomic UPDATE once the chosen candidate-target row exists.
-- name: MarkManagerDecisionGrab :exec
UPDATE manager_decisions SET verdict = 'would_grab', chosen_target_row = $2 WHERE id = $1;

-- name: CreateManagerCandidateTarget :one
INSERT INTO manager_candidate_targets (candidate_id, decision_id, run_id, verdict, rejections, selection_rank)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- ── History reads ────────────────────────────────────────────────────────

-- name: ListManagerRuns :many
SELECT * FROM manager_runs
WHERE (sqlc.arg(kinds)::text[] = '{}' OR kind = ANY(sqlc.arg(kinds)::text[]))
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountManagerRuns :one
SELECT count(*) FROM manager_runs
WHERE (sqlc.arg(kinds)::text[] = '{}' OR kind = ANY(sqlc.arg(kinds)::text[]));

-- name: GetManagerRun :one
SELECT * FROM manager_runs WHERE id = $1;

-- name: ListManagerRunIndexers :many
SELECT * FROM manager_run_indexers WHERE run_id = $1 ORDER BY indexer_name, domain;

-- name: ListManagerDecisionsByRun :many
SELECT * FROM manager_decisions WHERE run_id = $1 ORDER BY target_key;

-- name: ListManagerCandidatesByRun :many
SELECT * FROM manager_candidates WHERE run_id = $1 ORDER BY id;

-- name: ListManagerCandidateTargetsByRun :many
SELECT * FROM manager_candidate_targets WHERE run_id = $1 ORDER BY decision_id, selection_rank NULLS LAST, candidate_id;

-- Recent decisions across runs — the History page ledger. Keyset paging via
-- (decided_at, id) cursor keeps deep pages cheap.
-- name: ListManagerDecisions :many
SELECT d.*, r.kind AS run_kind, r.source AS run_source
FROM manager_decisions d
JOIN manager_runs r ON r.id = d.run_id
WHERE (sqlc.arg(verdicts)::text[] = '{}' OR d.verdict = ANY(sqlc.arg(verdicts)::text[]))
  AND (sqlc.arg(domains)::text[] = '{}' OR d.domain = ANY(sqlc.arg(domains)::text[]))
  AND (sqlc.arg(library_id)::bigint = 0 OR d.library_id = sqlc.arg(library_id)::bigint)
  AND (sqlc.narg(before_decided_at)::timestamptz IS NULL
       OR (d.decided_at, d.id) < (sqlc.narg(before_decided_at)::timestamptz, sqlc.arg(before_id)::bigint))
ORDER BY d.decided_at DESC, d.id DESC
LIMIT sqlc.arg(page_limit);

-- name: ListManagerDecisionsByItem :many
SELECT d.*, r.kind AS run_kind, r.source AS run_source
FROM manager_decisions d
JOIN manager_runs r ON r.id = d.run_id
WHERE d.media_item_id = $1
ORDER BY d.decided_at DESC, d.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountManagerDecisionsByItem :one
SELECT count(*) FROM manager_decisions WHERE media_item_id = $1;

-- Latest decision per media item, for wanted rows and entity panels.
-- name: GetLatestManagerDecisionForItem :one
SELECT * FROM manager_decisions
WHERE media_item_id = $1
ORDER BY decided_at DESC, id DESC
LIMIT 1;

-- ── Retention ────────────────────────────────────────────────────────────

-- name: PruneManagerRuns :execrows
DELETE FROM manager_runs WHERE started_at < $1;

-- name: PruneManagerReleases :execrows
DELETE FROM manager_releases WHERE last_seen_at < $1;

-- ── RSS cursors ──────────────────────────────────────────────────────────

-- name: GetManagerRSSCursor :one
SELECT * FROM manager_rss_cursors WHERE indexer_id = $1 AND domain = $2;

-- name: UpsertManagerRSSCursor :exec
INSERT INTO manager_rss_cursors (indexer_id, domain, last_release_key, last_publish_date, last_run_id, updated_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (indexer_id, domain) DO UPDATE
SET last_release_key = EXCLUDED.last_release_key,
    last_publish_date = EXCLUDED.last_publish_date,
    last_run_id = EXCLUDED.last_run_id,
    updated_at = now();

-- name: ListPendingManagerSightings :many
SELECT * FROM manager_release_sightings
WHERE run_id = $1 AND status = 'pending'
ORDER BY id;

-- name: GetManagerRelease :one
SELECT * FROM manager_releases WHERE id = $1;
