-- Heya Media Manager: indexers, download clients, quality profiles.

-- name: ListManagerIndexers :many
SELECT * FROM manager_indexers ORDER BY parent_id NULLS FIRST, priority ASC, name ASC;

-- name: GetManagerIndexer :one
SELECT * FROM manager_indexers WHERE id = $1;

-- name: CreateManagerIndexer :one
INSERT INTO manager_indexers (name, kind, enabled, base_url, api_key, protocol, priority, categories, source, source_ref, parent_id, settings)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdateManagerIndexer :one
UPDATE manager_indexers
SET name = $2, enabled = $3, base_url = $4, api_key = $5, protocol = $6,
    priority = $7, categories = $8, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteManagerIndexer :exec
DELETE FROM manager_indexers WHERE id = $1;

-- name: SetManagerIndexerTestResult :exec
UPDATE manager_indexers
SET last_test_at = now(), last_test_ok = $2, last_test_error = $3, updated_at = now()
WHERE id = $1;

-- Sync from a Prowlarr app row: children are keyed by (parent_id, source_ref).
-- name: UpsertManagerIndexerChild :one
INSERT INTO manager_indexers (name, kind, enabled, base_url, api_key, protocol, priority, categories, source, source_ref, parent_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (parent_id, source_ref) WHERE parent_id IS NOT NULL
DO UPDATE SET name = EXCLUDED.name, base_url = EXCLUDED.base_url,
    api_key = EXCLUDED.api_key, protocol = EXCLUDED.protocol,
    updated_at = now()
RETURNING *;

-- name: DeleteStaleManagerIndexerChildren :exec
DELETE FROM manager_indexers
WHERE parent_id = $1 AND NOT (source_ref = ANY(sqlc.arg(keep_refs)::text[]));

-- name: ListManagerDownloadClients :many
SELECT * FROM manager_download_clients ORDER BY priority ASC, name ASC;

-- name: GetManagerDownloadClient :one
SELECT * FROM manager_download_clients WHERE id = $1;

-- name: CreateManagerDownloadClient :one
INSERT INTO manager_download_clients (name, kind, enabled, protocol, base_url, api_key, username, password, category, priority, path_mappings, settings)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdateManagerDownloadClient :one
UPDATE manager_download_clients
SET name = $2, enabled = $3, base_url = $4, api_key = $5, username = $6,
    password = $7, category = $8, priority = $9, path_mappings = $10, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteManagerDownloadClient :exec
DELETE FROM manager_download_clients WHERE id = $1;

-- name: SetManagerDownloadClientTestResult :exec
UPDATE manager_download_clients
SET last_test_at = now(), last_test_ok = $2, last_test_error = $3, updated_at = now()
WHERE id = $1;

-- name: ListManagerQualityProfiles :many
SELECT * FROM manager_quality_profiles ORDER BY domain ASC, name ASC;

-- name: GetManagerQualityProfile :one
SELECT * FROM manager_quality_profiles WHERE id = $1;

-- name: CreateManagerQualityProfile :one
INSERT INTO manager_quality_profiles (name, domain, items, cutoff, upgrades_enabled, min_format_score, cutoff_format_score, min_upgrade_score, format_scores, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: UpdateManagerQualityProfile :one
UPDATE manager_quality_profiles
SET name = $2, items = $3, cutoff = $4, upgrades_enabled = $5,
    min_format_score = $6, cutoff_format_score = $7, min_upgrade_score = $8,
    format_scores = $9, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteManagerQualityProfile :exec
DELETE FROM manager_quality_profiles WHERE id = $1;

-- name: CountMediaItemsByQualityProfile :one
SELECT count(*) FROM media_items WHERE quality_profile_id = $1;

-- name: ListManagerCustomFormats :many
SELECT * FROM manager_custom_formats ORDER BY domain ASC, name ASC;

-- name: GetManagerCustomFormat :one
SELECT * FROM manager_custom_formats WHERE id = $1;

-- name: GetManagerCustomFormatByName :one
SELECT * FROM manager_custom_formats WHERE domain = $1 AND name = $2;

-- name: CreateManagerCustomFormat :one
INSERT INTO manager_custom_formats (name, domain, include_when_renaming, specifications, trash_id, source)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateManagerCustomFormat :one
UPDATE manager_custom_formats
SET name = $2, include_when_renaming = $3, specifications = $4, trash_id = $5,
    source = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteManagerCustomFormat :exec
DELETE FROM manager_custom_formats WHERE id = $1;
