-- name: UpsertMetadataEntityBinding :one
INSERT INTO metadata_entity_bindings (
  local_kind, local_id, entity_id, entity_kind, schema_version, projection_version
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (local_kind, local_id) DO UPDATE SET
  entity_id = EXCLUDED.entity_id,
  entity_kind = EXCLUDED.entity_kind,
  schema_version = EXCLUDED.schema_version,
  projection_version = CASE
    WHEN metadata_entity_bindings.entity_id = EXCLUDED.entity_id
      THEN GREATEST(metadata_entity_bindings.projection_version, EXCLUDED.projection_version)
    ELSE EXCLUDED.projection_version
  END,
  updated_at = now()
RETURNING *;

-- name: GetMetadataEntityBinding :one
SELECT *
FROM metadata_entity_bindings
WHERE local_kind = $1 AND local_id = $2;

-- name: GetMediaItemMetadataBinding :one
SELECT *
FROM metadata_entity_bindings
WHERE local_kind = 'media_item' AND local_id = $1;

-- name: PromoteCanonicalMetadataProviderID :exec
UPDATE local_media_identities
SET metadata_provider_id = $2,
    updated_at = now()
WHERE media_item_id = $1
  AND metadata_provider_id IS DISTINCT FROM $2;

-- name: ListMetadataBindingsByEntity :many
SELECT *
FROM metadata_entity_bindings
WHERE entity_id = $1
ORDER BY local_kind, local_id;

-- name: ListMetadataBindingsByLocalIDs :many
SELECT *
FROM metadata_entity_bindings
WHERE local_kind = $1 AND local_id = ANY($2::bigint[])
ORDER BY local_id;

-- name: UpsertMetadataWorkflow :one
INSERT INTO metadata_resolution_workflows (
  request_key, identity_id, kind, query, hints, selected_resolution, state
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (request_key) DO UPDATE SET
  identity_id = COALESCE(EXCLUDED.identity_id, metadata_resolution_workflows.identity_id),
  query = CASE WHEN EXCLUDED.query <> '' THEN EXCLUDED.query ELSE metadata_resolution_workflows.query END,
  hints = CASE WHEN EXCLUDED.hints <> '{}'::jsonb THEN EXCLUDED.hints ELSE metadata_resolution_workflows.hints END,
  selected_resolution = CASE WHEN EXCLUDED.selected_resolution <> '{}'::jsonb THEN EXCLUDED.selected_resolution ELSE metadata_resolution_workflows.selected_resolution END,
  updated_at = now()
RETURNING *;

-- name: GetMetadataWorkflowByKey :one
SELECT *
FROM metadata_resolution_workflows
WHERE request_key = $1;

-- name: MarkMetadataWorkflowDiscovery :one
UPDATE metadata_resolution_workflows
SET discovery_id = $2,
    state = $3,
    last_error = '',
    updated_at = now()
WHERE request_key = $1
RETURNING *;

-- name: ClearMetadataWorkflowDiscovery :exec
UPDATE metadata_resolution_workflows
SET discovery_id = NULL,
    state = 'pending',
    last_error = '',
    updated_at = now()
WHERE request_key = $1;

-- name: MarkMetadataWorkflowResolving :one
UPDATE metadata_resolution_workflows
SET selected_resolution = $2,
    job_id = $3,
    state = 'resolving',
    last_error = '',
    updated_at = now()
WHERE request_key = $1
RETURNING *;

-- name: CompleteMetadataWorkflow :one
UPDATE metadata_resolution_workflows
SET entity_id = $2,
    state = 'completed',
    last_error = '',
    completed_at = now(),
    updated_at = now()
WHERE request_key = $1
RETURNING *;

-- name: FailMetadataWorkflow :one
UPDATE metadata_resolution_workflows
SET state = 'failed',
    last_error = $2,
    updated_at = now()
WHERE request_key = $1
RETURNING *;

-- name: GetMetadataChangeCursor :one
SELECT next_cursor
FROM metadata_change_consumers
WHERE consumer = $1;

-- name: CommitMetadataChangeCursor :exec
INSERT INTO metadata_change_consumers (consumer, next_cursor)
VALUES ($1, $2)
ON CONFLICT (consumer) DO UPDATE SET
  next_cursor = GREATEST(metadata_change_consumers.next_cursor, EXCLUDED.next_cursor),
  updated_at = now();
