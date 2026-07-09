-- name: CreateScanRun :one
INSERT INTO scan_runs (library_id, media_type, scanner_version, mode, status, summary)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: FinishScanRun :exec
UPDATE scan_runs
SET status = $2,
    summary = $3,
    error_message = $4,
    finished_at = now()
WHERE id = $1;

-- name: UpsertScanRunArtifact :one
INSERT INTO scan_run_artifacts (scan_run_id, kind, scope_key, schema_version, data)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (scan_run_id, kind, scope_key) DO UPDATE
SET schema_version = EXCLUDED.schema_version,
    data = EXCLUDED.data
RETURNING *;

-- name: GetScanRunArtifact :one
SELECT * FROM scan_run_artifacts
WHERE scan_run_id = $1
  AND kind = $2
  AND scope_key = $3
ORDER BY id DESC
LIMIT 1;

-- name: GetLatestScanRunArtifactByLibrary :one
SELECT sra.*
FROM scan_run_artifacts sra
JOIN scan_runs sr ON sr.id = sra.scan_run_id
WHERE sr.library_id = $1
  AND sr.media_type = $2
  AND sr.status = 'complete'
  AND sra.kind = $3
  AND sra.scope_key = $4
ORDER BY sr.started_at DESC, sr.id DESC, sra.id DESC
LIMIT 1;

-- name: CompactAppliedScannerEntityArtifacts :one
WITH target AS (
    UPDATE scanner_entities entity
    SET search_artifact_id = NULL,
        metadata_artifact_id = NULL,
        apply_artifact_id = NULL,
        updated_at = now()
    WHERE entity.id = $1
      AND entity.status = 'applied'
    RETURNING id
),
deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING target
    WHERE artifact.entity_id = target.id
    RETURNING artifact.id
)
SELECT count(*)::bigint AS deleted_count FROM deleted;

-- name: CompactAppliedScannerArtifactsForEntity :one
WITH target AS (
    SELECT entity.id, entity.scope_key
    FROM scanner_entities entity
    WHERE entity.id = $1
      AND entity.status = 'applied'
),
candidate_runs AS (
    SELECT entity.search_scan_run_id AS scan_run_id, target.scope_key
    FROM scanner_entities entity
    JOIN target ON target.id = entity.id
    WHERE entity.search_scan_run_id IS NOT NULL
    UNION
    SELECT entity.fetch_scan_run_id AS scan_run_id, target.scope_key
    FROM scanner_entities entity
    JOIN target ON target.id = entity.id
    WHERE entity.fetch_scan_run_id IS NOT NULL
    UNION
    SELECT artifact.scan_run_id, target.scope_key
    FROM scanner_entity_artifacts artifact
    JOIN target ON target.id = artifact.entity_id
    WHERE artifact.scan_run_id IS NOT NULL
),
safe_runs AS (
    SELECT DISTINCT candidate_runs.scan_run_id, candidate_runs.scope_key
    FROM candidate_runs
    JOIN scan_runs ON scan_runs.id = candidate_runs.scan_run_id
    WHERE scan_runs.finished_at IS NOT NULL
      AND NOT EXISTS (
        SELECT 1
        FROM scanner_entities peer
        WHERE peer.scope_key = candidate_runs.scope_key
          AND peer.status <> 'applied'
          AND (
            peer.search_scan_run_id = candidate_runs.scan_run_id
            OR peer.fetch_scan_run_id = candidate_runs.scan_run_id
            OR EXISTS (
                SELECT 1
                FROM scanner_entity_artifacts peer_artifact
                WHERE peer_artifact.entity_id = peer.id
                  AND peer_artifact.scan_run_id = candidate_runs.scan_run_id
            )
          )
      )
),
scan_deleted AS (
    DELETE FROM scan_run_artifacts artifact
    USING safe_runs
    WHERE artifact.scan_run_id = safe_runs.scan_run_id
      AND artifact.scope_key = safe_runs.scope_key
    RETURNING artifact.id
),
entity_artifacts_deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING target
    WHERE artifact.entity_id = target.id
    RETURNING artifact.id
),
updated AS (
    UPDATE scanner_entities entity
    SET search_artifact_id = NULL,
        metadata_artifact_id = NULL,
        apply_artifact_id = NULL,
        updated_at = now()
    FROM target
    WHERE entity.id = target.id
    RETURNING entity.id
)
SELECT
    (SELECT count(*) FROM entity_artifacts_deleted)::bigint AS entity_artifacts_deleted,
    (SELECT count(*) FROM scan_deleted)::bigint AS scan_run_artifacts_deleted;

-- name: CleanupFullyAppliedScanRunArtifactsForEntity :one
WITH candidate_runs AS (
    SELECT search_scan_run_id AS scan_run_id
    FROM scanner_entities entity
    WHERE entity.id = $1
      AND entity.search_scan_run_id IS NOT NULL
    UNION
    SELECT fetch_scan_run_id AS scan_run_id
    FROM scanner_entities entity
    WHERE entity.id = $1
      AND entity.fetch_scan_run_id IS NOT NULL
),
safe_runs AS (
    SELECT candidate_runs.scan_run_id
    FROM candidate_runs
    JOIN scan_runs ON scan_runs.id = candidate_runs.scan_run_id
    WHERE scan_runs.finished_at IS NOT NULL
      AND NOT EXISTS (
        SELECT 1
        FROM scanner_entities entity
        WHERE (entity.search_scan_run_id = candidate_runs.scan_run_id
            OR entity.fetch_scan_run_id = candidate_runs.scan_run_id)
          AND entity.status <> 'applied'
      )
),
deleted AS (
    DELETE FROM scan_run_artifacts artifact
    USING safe_runs
    WHERE artifact.scan_run_id = safe_runs.scan_run_id
    RETURNING artifact.id
)
SELECT count(*)::bigint AS deleted_count FROM deleted;

-- name: CleanupCompletedScanRunArtifactsForAppliedScopes :one
WITH deletable AS (
    SELECT artifact.id
    FROM scan_run_artifacts artifact
    JOIN scan_runs ON scan_runs.id = artifact.scan_run_id
    WHERE scan_runs.finished_at IS NOT NULL
      AND EXISTS (
        SELECT 1
        FROM scanner_entities entity
        WHERE entity.library_id = scan_runs.library_id
          AND entity.media_type = scan_runs.media_type
          AND entity.scope_key = artifact.scope_key
      )
      AND NOT EXISTS (
        SELECT 1
        FROM scanner_entities entity
        WHERE entity.library_id = scan_runs.library_id
          AND entity.media_type = scan_runs.media_type
          AND entity.scope_key = artifact.scope_key
          AND entity.status <> 'applied'
      )
),
deleted AS (
    DELETE FROM scan_run_artifacts artifact
    USING deletable
    WHERE artifact.id = deletable.id
    RETURNING artifact.id
)
SELECT count(*)::bigint AS deleted_count FROM deleted;

-- name: CleanupAppliedScannerEntityArtifactsOlderThan :one
WITH target AS (
    UPDATE scanner_entities
    SET search_artifact_id = NULL,
        metadata_artifact_id = NULL,
        apply_artifact_id = NULL,
        updated_at = now()
    WHERE status = 'applied'
      AND applied_at IS NOT NULL
      AND applied_at < sqlc.arg(cutoff_at)
    RETURNING id
),
deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING target
    WHERE artifact.entity_id = target.id
    RETURNING artifact.id
)
SELECT count(*)::bigint AS deleted_count FROM deleted;

-- name: CleanupStaleInFlightScannerEntitiesOlderThan :one
WITH target AS (
    SELECT entity.id, entity.library_id, entity.media_type, entity.scope_key, entity.search_scan_run_id, entity.fetch_scan_run_id
    FROM scanner_entities entity
    WHERE entity.status IN ('matched', 'fetching')
      AND entity.updated_at < sqlc.arg(cutoff_at)
),
target_runs AS (
    SELECT library_id, media_type, scope_key, search_scan_run_id AS scan_run_id
    FROM target
    WHERE search_scan_run_id IS NOT NULL
    UNION
    SELECT library_id, media_type, scope_key, fetch_scan_run_id AS scan_run_id
    FROM target
    WHERE fetch_scan_run_id IS NOT NULL
    UNION
    SELECT target.library_id, target.media_type, target.scope_key, artifact.scan_run_id
    FROM scanner_entity_artifacts artifact
    JOIN target ON target.id = artifact.entity_id
    WHERE artifact.scan_run_id IS NOT NULL
),
scan_deleted AS (
    DELETE FROM scan_run_artifacts artifact
    USING target_runs, scan_runs
    WHERE artifact.scan_run_id = target_runs.scan_run_id
      AND artifact.scope_key = target_runs.scope_key
      AND scan_runs.id = artifact.scan_run_id
      AND scan_runs.finished_at IS NOT NULL
      AND NOT EXISTS (
        SELECT 1
        FROM scanner_entities peer
        WHERE peer.library_id = target_runs.library_id
          AND peer.media_type = target_runs.media_type
          AND peer.scope_key = target_runs.scope_key
          AND NOT EXISTS (
            SELECT 1
            FROM target
            WHERE target.id = peer.id
          )
          AND (
            peer.search_scan_run_id = target_runs.scan_run_id
            OR peer.fetch_scan_run_id = target_runs.scan_run_id
            OR EXISTS (
                SELECT 1
                FROM scanner_entity_artifacts peer_artifact
                WHERE peer_artifact.entity_id = peer.id
                  AND peer_artifact.scan_run_id = target_runs.scan_run_id
            )
          )
      )
    RETURNING artifact.id
),
entity_artifacts_deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING target
    WHERE artifact.entity_id = target.id
    RETURNING artifact.id
),
entities_deleted AS (
    DELETE FROM scanner_entities entity
    USING target
    WHERE entity.id = target.id
    RETURNING entity.id
)
SELECT
    (SELECT count(*) FROM entities_deleted)::bigint AS entities_deleted,
    (SELECT count(*) FROM entity_artifacts_deleted)::bigint AS entity_artifacts_deleted,
    (SELECT count(*) FROM scan_deleted)::bigint AS scan_run_artifacts_deleted;

-- name: CleanupOldScanRunArtifacts :one
WITH deleted AS (
    DELETE FROM scan_run_artifacts artifact
    USING scan_runs
    WHERE artifact.scan_run_id = scan_runs.id
      AND scan_runs.finished_at IS NOT NULL
      AND artifact.created_at < sqlc.arg(cutoff_at)
    RETURNING artifact.id
)
SELECT count(*)::bigint AS deleted_count FROM deleted;

-- name: UpsertScannerEntity :one
INSERT INTO scanner_entities (
    library_id, media_type, scope_key, scope_paths, identity_key,
    title, year, provider_id, status, search_scan_run_id,
    search_artifact_id, error_message, data
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13
)
ON CONFLICT (library_id, media_type, scope_key, identity_key) DO UPDATE
SET scope_paths = EXCLUDED.scope_paths,
    title = EXCLUDED.title,
    year = EXCLUDED.year,
    provider_id = EXCLUDED.provider_id,
    status = EXCLUDED.status,
    search_scan_run_id = EXCLUDED.search_scan_run_id,
    search_artifact_id = EXCLUDED.search_artifact_id,
    fetch_scan_run_id = NULL,
    metadata_artifact_id = NULL,
    apply_artifact_id = NULL,
    error_message = EXCLUDED.error_message,
    data = EXCLUDED.data,
    searched_at = CASE WHEN EXCLUDED.search_artifact_id IS NOT NULL THEN now() ELSE scanner_entities.searched_at END,
    fetched_at = NULL,
    applied_at = NULL,
    updated_at = now()
RETURNING *;

-- name: GetScannerEntity :one
SELECT * FROM scanner_entities
WHERE id = $1;

-- name: CreateScannerEntityArtifact :one
INSERT INTO scanner_entity_artifacts (entity_id, stage, schema_version, scan_run_id, data)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetScannerEntityArtifact :one
SELECT * FROM scanner_entity_artifacts
WHERE id = $1;

-- name: GetLatestScannerEntityArtifact :one
SELECT * FROM scanner_entity_artifacts
WHERE entity_id = $1
  AND stage = $2
ORDER BY id DESC
LIMIT 1;

-- name: MarkScannerEntityFetching :one
UPDATE scanner_entities
SET status = 'fetching',
    error_message = '',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkScannerEntityFetched :one
UPDATE scanner_entities
SET status = $2,
    fetch_scan_run_id = $3,
    metadata_artifact_id = $4,
    error_message = $5,
    fetched_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkScannerEntityApplying :one
UPDATE scanner_entities
SET status = 'applying',
    error_message = '',
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkScannerEntityApplied :one
UPDATE scanner_entities
SET status = $2,
    apply_artifact_id = $3,
    error_message = $4,
    applied_at = CASE WHEN $2 = 'applied' THEN now() ELSE applied_at END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkScannerEntityFailed :one
UPDATE scanner_entities
SET status = $2,
    error_message = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpsertLocalMediaIdentity :one
INSERT INTO local_media_identities (
    library_id, media_type, identity_key, title, year, confidence, source,
    review_status, metadata_provider_id, media_item_id,
    first_seen_scan_run_id, last_seen_scan_run_id, raw_identity
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (library_id, media_type, identity_key) DO UPDATE
SET title = EXCLUDED.title,
    year = EXCLUDED.year,
    confidence = EXCLUDED.confidence,
    source = EXCLUDED.source,
    review_status = EXCLUDED.review_status,
    metadata_provider_id = COALESCE(NULLIF(EXCLUDED.metadata_provider_id, ''), local_media_identities.metadata_provider_id),
    media_item_id = COALESCE(EXCLUDED.media_item_id, local_media_identities.media_item_id),
    last_seen_scan_run_id = EXCLUDED.last_seen_scan_run_id,
    raw_identity = EXCLUDED.raw_identity,
    updated_at = now()
RETURNING *;

-- name: UpsertLocalMediaIdentityExternalID :exec
INSERT INTO local_media_identity_external_ids (identity_id, provider, external_id, source)
VALUES ($1, $2, $3, $4)
ON CONFLICT (identity_id, provider) DO UPDATE
SET external_id = EXCLUDED.external_id,
    source = EXCLUDED.source,
    updated_at = now();

-- name: UpsertMetadataMatchCandidate :one
INSERT INTO metadata_match_candidates (
    identity_id, scan_run_id, provider_name, provider_id, provider_kind,
    title, year, score, rank, status, rejection_reason, external_ids, raw_data
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (identity_id, provider_id) DO UPDATE
SET scan_run_id = EXCLUDED.scan_run_id,
    provider_name = EXCLUDED.provider_name,
    provider_kind = EXCLUDED.provider_kind,
    title = EXCLUDED.title,
    year = EXCLUDED.year,
    score = EXCLUDED.score,
    rank = EXCLUDED.rank,
    status = EXCLUDED.status,
    rejection_reason = EXCLUDED.rejection_reason,
    external_ids = EXCLUDED.external_ids,
    raw_data = EXCLUDED.raw_data,
    updated_at = now()
RETURNING *;

-- name: DeleteMetadataMatchCandidatesByIdentity :exec
DELETE FROM metadata_match_candidates WHERE identity_id = $1;

-- name: CreateScanFinding :one
INSERT INTO scan_findings (
    scan_run_id, library_id, media_type, identity_id, media_item_id,
    library_file_id, severity, code, rel_path, message, data
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: ResolveScanFinding :exec
UPDATE scan_findings SET resolved_at = now() WHERE id = $1;

-- name: ResolveOpenScanFindingsByLibrary :exec
UPDATE scan_findings
SET resolved_at = now()
WHERE library_id = $1
  AND media_type = $2
  AND resolved_at IS NULL
  AND code = ANY($3::text[]);

-- name: DeleteLibraryFileLinksByFile :exec
DELETE FROM library_file_links WHERE library_file_id = $1;

-- name: CreateLibraryFileLink :one
INSERT INTO library_file_links (
    library_file_id, media_item_id, movie_id, tv_episode_id, relation_type,
    season_number, episode_number, absolute_number, part_index,
    title, source, confidence
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: CreateLibraryFileExtraLink :one
INSERT INTO library_file_links (
    library_file_id, media_item_id, relation_type, extra_type,
    title, source, confidence, metadata
)
VALUES ($1, $2, 'extra', $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListLibraryFileLinksByMediaItem :many
SELECT * FROM library_file_links
WHERE media_item_id = $1
ORDER BY relation_type, season_number NULLS FIRST, episode_number NULLS FIRST, part_index NULLS FIRST, id;

-- name: ListLibraryFileLinksByFile :many
SELECT * FROM library_file_links
WHERE library_file_id = $1
ORDER BY relation_type, season_number NULLS FIRST, episode_number NULLS FIRST, part_index NULLS FIRST, id;

-- name: ListTVEpisodeLinkTargetsByMediaItem :many
SELECT e.id AS episode_id, s.season_number, e.episode_number, e.absolute_number
FROM tv_episodes e
JOIN tv_seasons s ON s.id = e.season_id
JOIN tv_series ts ON ts.id = s.series_id
WHERE ts.media_item_id = $1;

-- name: UpsertMediaItemExternalID :exec
WITH entity AS (
  SELECT media_items.id, media_items.library_id FROM media_items WHERE media_items.id = $1
)
INSERT INTO media_item_external_ids (media_item_id, library_id, provider, external_id, source)
SELECT entity.id, entity.library_id, $2, $3, $4
FROM entity
ON CONFLICT (media_item_id, provider) DO UPDATE
SET library_id = EXCLUDED.library_id,
    external_id = EXCLUDED.external_id,
    source = EXCLUDED.source,
    updated_at = now();

-- name: ListMediaItemExternalIDs :many
SELECT * FROM media_item_external_ids
WHERE media_item_id = $1
ORDER BY provider;

-- name: GetMediaItemByNormalizedExternalID :one
SELECT mi.*
FROM media_item_external_ids ei
JOIN media_item_cards mi ON mi.id = ei.media_item_id
WHERE mi.library_id = $1
  AND ei.provider = $2
  AND ei.external_id = $3
ORDER BY mi.id
LIMIT 1;

-- name: ListScannerRunsByLibrary :many
SELECT * FROM scan_runs
WHERE library_id = $1
ORDER BY started_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: GetLatestScannerRunByLibrary :one
SELECT * FROM scan_runs
WHERE library_id = $1
ORDER BY started_at DESC, id DESC
LIMIT 1;

-- name: ListOpenScannerFindingsByLibrary :many
SELECT
    sf.*,
    lmi.identity_key,
    lmi.title AS identity_title,
    lmi.year AS identity_year,
    mi.title AS media_title
FROM scan_findings sf
LEFT JOIN local_media_identities lmi ON lmi.id = sf.identity_id
LEFT JOIN media_item_cards mi ON mi.id = sf.media_item_id
WHERE sf.library_id = $1
  AND sf.resolved_at IS NULL
ORDER BY
    CASE sf.severity WHEN 'error' THEN 0 WHEN 'warn' THEN 1 ELSE 2 END,
    sf.created_at DESC,
    sf.id DESC;

-- name: ListScannerIdentitiesByLibrary :many
SELECT
    lmi.*,
    COALESCE(selected.provider_id, '') AS selected_provider_id,
    COALESCE(selected.title, '') AS selected_title,
    COALESCE(selected.year, '') AS selected_year,
    selected.score AS selected_score,
    COALESCE(candidate_counts.candidate_count, 0)::bigint AS candidate_count,
    COALESCE(open_finding_counts.open_finding_count, 0)::bigint AS open_finding_count
FROM local_media_identities lmi
LEFT JOIN LATERAL (
    SELECT provider_id, title, year, score
    FROM metadata_match_candidates mmc
    WHERE mmc.identity_id = lmi.id
      AND mmc.status = 'selected'
    ORDER BY mmc.rank, mmc.score DESC NULLS LAST, mmc.id
    LIMIT 1
) selected ON true
LEFT JOIN LATERAL (
    SELECT count(*) AS candidate_count
    FROM metadata_match_candidates mmc
    WHERE mmc.identity_id = lmi.id
) candidate_counts ON true
LEFT JOIN LATERAL (
    SELECT count(*) AS open_finding_count
    FROM scan_findings sf
    WHERE sf.identity_id = lmi.id
      AND sf.resolved_at IS NULL
) open_finding_counts ON true
WHERE lmi.library_id = $1
ORDER BY
    CASE lmi.review_status WHEN 'rejected' THEN 0 WHEN 'review' THEN 1 WHEN 'suspicious' THEN 2 ELSE 3 END,
    lmi.title,
    lmi.year,
    lmi.id;

-- name: GetScannerIdentityForView :one
SELECT
    lmi.*,
    COALESCE(selected.provider_id, '') AS selected_provider_id,
    COALESCE(selected.title, '') AS selected_title,
    COALESCE(selected.year, '') AS selected_year,
    selected.score AS selected_score,
    COALESCE(candidate_counts.candidate_count, 0)::bigint AS candidate_count,
    COALESCE(open_finding_counts.open_finding_count, 0)::bigint AS open_finding_count
FROM local_media_identities lmi
LEFT JOIN LATERAL (
    SELECT provider_id, title, year, score
    FROM metadata_match_candidates mmc
    WHERE mmc.identity_id = lmi.id
      AND mmc.status = 'selected'
    ORDER BY mmc.rank, mmc.score DESC NULLS LAST, mmc.id
    LIMIT 1
) selected ON true
LEFT JOIN LATERAL (
    SELECT count(*) AS candidate_count
    FROM metadata_match_candidates mmc
    WHERE mmc.identity_id = lmi.id
) candidate_counts ON true
LEFT JOIN LATERAL (
    SELECT count(*) AS open_finding_count
    FROM scan_findings sf
    WHERE sf.identity_id = lmi.id
      AND sf.resolved_at IS NULL
) open_finding_counts ON true
WHERE lmi.library_id = sqlc.arg(library_id)
  AND lmi.id = sqlc.arg(identity_id);

-- name: ListScannerCandidatesByLibrary :many
SELECT
    mmc.*,
    lmi.identity_key,
    lmi.title AS identity_title,
    lmi.year AS identity_year
FROM metadata_match_candidates mmc
JOIN local_media_identities lmi ON lmi.id = mmc.identity_id
WHERE lmi.library_id = $1
ORDER BY lmi.title, lmi.year, mmc.rank, mmc.id;

-- name: ListScannerSearchDecisionsByLibrary :many
SELECT
    lmi.identity_key,
    lmi.review_status,
    lmi.metadata_provider_id,
    COALESCE(selected.provider_name, '')::text AS provider_name,
    COALESCE(NULLIF(selected.provider_id, ''), lmi.metadata_provider_id)::text AS provider_id,
    COALESCE(NULLIF(selected.title, ''), lmi.title)::text AS title,
    COALESCE(NULLIF(selected.year, ''), lmi.year)::text AS year,
    selected.score,
    COALESCE(selected.external_ids, '{}'::jsonb) AS external_ids
FROM local_media_identities lmi
LEFT JOIN LATERAL (
    SELECT provider_name, provider_id, title, year, score, external_ids
    FROM metadata_match_candidates mmc
    WHERE mmc.identity_id = lmi.id
      AND (
        mmc.provider_id = lmi.metadata_provider_id
        OR mmc.status = 'selected'
      )
    ORDER BY
        CASE WHEN mmc.provider_id = lmi.metadata_provider_id THEN 0 ELSE 1 END,
        CASE WHEN mmc.status = 'selected' THEN 0 ELSE 1 END,
        mmc.rank,
        mmc.score DESC,
        mmc.id
    LIMIT 1
) selected ON true
WHERE lmi.library_id = sqlc.arg(library_id)
  AND lmi.media_type = sqlc.arg(media_type)
  AND lmi.review_status = ANY(sqlc.arg(review_statuses)::text[])
ORDER BY lmi.identity_key;

-- name: ApproveScannerCandidate :one
WITH candidate AS (
    SELECT mmc.*
    FROM metadata_match_candidates mmc
    JOIN local_media_identities lmi ON lmi.id = mmc.identity_id
    WHERE mmc.id = sqlc.arg(candidate_id)
      AND mmc.identity_id = sqlc.arg(identity_id)
      AND lmi.library_id = sqlc.arg(library_id)
),
updated_identity AS (
    UPDATE local_media_identities lmi
    SET review_status = 'accepted',
        metadata_provider_id = candidate.provider_id,
        updated_at = now()
    FROM candidate
    WHERE lmi.id = sqlc.arg(identity_id)
      AND lmi.library_id = sqlc.arg(library_id)
    RETURNING lmi.*
),
demoted AS (
    UPDATE metadata_match_candidates
    SET status = 'candidate',
        rejection_reason = '',
        updated_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND id <> sqlc.arg(candidate_id)
      AND EXISTS (SELECT 1 FROM candidate)
    RETURNING 1
),
selected AS (
    UPDATE metadata_match_candidates
    SET status = 'selected',
        rejection_reason = '',
        updated_at = now()
    WHERE id = sqlc.arg(candidate_id)
      AND identity_id = sqlc.arg(identity_id)
      AND EXISTS (SELECT 1 FROM candidate)
    RETURNING 1
),
resolved AS (
    UPDATE scan_findings
    SET resolved_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND resolved_at IS NULL
      AND EXISTS (SELECT 1 FROM candidate)
    RETURNING 1
)
SELECT updated_identity.*
FROM updated_identity
CROSS JOIN (SELECT count(*) FROM demoted) demoted_count
CROSS JOIN (SELECT count(*) FROM selected) selected_count
CROSS JOIN (SELECT count(*) FROM resolved) resolved_count;

-- name: RejectScannerIdentity :one
WITH updated_identity AS (
    UPDATE local_media_identities lmi
    SET review_status = 'rejected',
        updated_at = now()
    WHERE lmi.library_id = sqlc.arg(library_id)
      AND lmi.id = sqlc.arg(identity_id)
    RETURNING lmi.*
),
candidates AS (
    UPDATE metadata_match_candidates
    SET status = 'rejected',
        rejection_reason = sqlc.arg(reason),
        updated_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND EXISTS (SELECT 1 FROM updated_identity)
    RETURNING 1
),
resolved AS (
    UPDATE scan_findings
    SET resolved_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND resolved_at IS NULL
      AND EXISTS (SELECT 1 FROM updated_identity)
    RETURNING 1
)
SELECT updated_identity.*
FROM updated_identity
CROSS JOIN (SELECT count(*) FROM candidates) candidate_count
CROSS JOIN (SELECT count(*) FROM resolved) resolved_count;

-- name: IgnoreScannerIdentity :one
WITH updated_identity AS (
    UPDATE local_media_identities lmi
    SET review_status = 'ignored',
        updated_at = now()
    WHERE lmi.library_id = sqlc.arg(library_id)
      AND lmi.id = sqlc.arg(identity_id)
    RETURNING lmi.*
),
candidates AS (
    UPDATE metadata_match_candidates
    SET status = 'ignored',
        rejection_reason = sqlc.arg(reason),
        updated_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND EXISTS (SELECT 1 FROM updated_identity)
    RETURNING 1
),
resolved AS (
    UPDATE scan_findings
    SET resolved_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND resolved_at IS NULL
      AND EXISTS (SELECT 1 FROM updated_identity)
    RETURNING 1
)
SELECT updated_identity.*
FROM updated_identity
CROSS JOIN (SELECT count(*) FROM candidates) candidate_count
CROSS JOIN (SELECT count(*) FROM resolved) resolved_count;

-- name: ResetScannerIdentityReview :one
WITH updated_identity AS (
    UPDATE local_media_identities lmi
    SET review_status = 'needs_review',
        metadata_provider_id = '',
        media_item_id = NULL,
        updated_at = now()
    WHERE lmi.library_id = sqlc.arg(library_id)
      AND lmi.id = sqlc.arg(identity_id)
    RETURNING lmi.*
),
candidates AS (
    UPDATE metadata_match_candidates
    SET status = 'candidate',
        rejection_reason = '',
        updated_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND EXISTS (SELECT 1 FROM updated_identity)
    RETURNING 1
),
resolved AS (
    UPDATE scan_findings
    SET resolved_at = now()
    WHERE identity_id = sqlc.arg(identity_id)
      AND resolved_at IS NULL
      AND EXISTS (SELECT 1 FROM updated_identity)
    RETURNING 1
)
SELECT updated_identity.*
FROM updated_identity
CROSS JOIN (SELECT count(*) FROM candidates) candidate_count
CROSS JOIN (SELECT count(*) FROM resolved) resolved_count;

-- name: ListMediaExtraLinks :many
SELECT
    l.id,
    l.media_item_id,
    COALESCE(NULLIF(l.extra_type, ''), 'other')::text AS extra_type,
    COALESCE(NULLIF(l.title, ''), regexp_replace(regexp_replace(lf.path, '^.*/', ''), '\.[^.]*$', ''))::text AS title,
    lf.path AS file_path,
    CASE
        WHEN l.metadata->>'duration_ms' ~ '^[0-9]+$' THEN (l.metadata->>'duration_ms')::integer
        ELSE 0
    END AS duration_ms,
    lf.size AS file_size,
    l.thumbnail_path,
    l.created_at
FROM library_file_links l
JOIN library_files lf ON lf.id = l.library_file_id
WHERE l.media_item_id = $1
  AND l.relation_type = 'extra'
  AND lf.deleted_at IS NULL
ORDER BY 3, 4, l.id;

-- name: GetMediaExtraLinkByID :one
SELECT
    l.id,
    l.media_item_id,
    COALESCE(NULLIF(l.extra_type, ''), 'other')::text AS extra_type,
    COALESCE(NULLIF(l.title, ''), regexp_replace(regexp_replace(lf.path, '^.*/', ''), '\.[^.]*$', ''))::text AS title,
    lf.path AS file_path,
    CASE
        WHEN l.metadata->>'duration_ms' ~ '^[0-9]+$' THEN (l.metadata->>'duration_ms')::integer
        ELSE 0
    END AS duration_ms,
    lf.size AS file_size,
    l.thumbnail_path,
    l.created_at
FROM library_file_links l
JOIN library_files lf ON lf.id = l.library_file_id
WHERE l.id = $1
  AND l.relation_type = 'extra'
  AND lf.deleted_at IS NULL;

-- name: UpdateMediaExtraLinkThumbnail :exec
UPDATE library_file_links
SET thumbnail_path = $2,
    updated_at = now()
WHERE id = $1
  AND relation_type = 'extra';
