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

-- name: UpsertMetadataEntityBindings :exec
-- Batch canonical bindings created while materializing rich metadata. One
-- local row can only bind to one canonical entity; a changed entity resets the
-- projection version while a refresh of the same entity advances it.
WITH input AS MATERIALIZED (
  SELECT COALESCE(value->>'local_kind', '') AS local_kind,
         (value->>'local_id')::bigint AS local_id,
         (value->>'entity_id')::uuid AS entity_id,
         COALESCE(value->>'entity_kind', '') AS entity_kind,
         COALESCE((value->>'schema_version')::integer, 1) AS schema_version,
         COALESCE((value->>'projection_version')::bigint, 0) AS projection_version
  FROM jsonb_array_elements(sqlc.arg(bindings)::jsonb) AS value
)
INSERT INTO metadata_entity_bindings (
  local_kind, local_id, entity_id, entity_kind, schema_version, projection_version
)
SELECT local_kind, local_id, entity_id, entity_kind, schema_version, projection_version
FROM input
ON CONFLICT (local_kind, local_id) DO UPDATE SET
  entity_id = EXCLUDED.entity_id,
  entity_kind = EXCLUDED.entity_kind,
  schema_version = EXCLUDED.schema_version,
  projection_version = CASE
    WHEN metadata_entity_bindings.entity_id = EXCLUDED.entity_id
      THEN GREATEST(metadata_entity_bindings.projection_version, EXCLUDED.projection_version)
    ELSE EXCLUDED.projection_version
  END,
  updated_at = now();

-- name: GetMetadataEntityBinding :one
SELECT *
FROM metadata_entity_bindings
WHERE local_kind = $1 AND local_id = $2;

-- name: GetMediaItemMetadataBinding :one
SELECT *
FROM metadata_entity_bindings
WHERE local_kind = 'media_item' AND local_id = $1;

-- name: GetMetadataEntityBindingForUpdate :one
SELECT *
FROM metadata_entity_bindings
WHERE local_kind = $1 AND local_id = $2
FOR UPDATE;

-- name: UpsertMetadataProjectionState :one
INSERT INTO metadata_projection_states (
  local_kind, local_id, scope, entity_id, entity_kind, projection_version
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (local_kind, local_id, scope) DO UPDATE SET
  entity_id = EXCLUDED.entity_id,
  entity_kind = EXCLUDED.entity_kind,
  projection_version = CASE
    WHEN metadata_projection_states.entity_id = EXCLUDED.entity_id
      THEN GREATEST(metadata_projection_states.projection_version, EXCLUDED.projection_version)
    ELSE EXCLUDED.projection_version
  END,
  applied_at = now()
RETURNING *;

-- name: GetMetadataProjectionState :one
SELECT *
FROM metadata_projection_states
WHERE local_kind = $1 AND local_id = $2 AND scope = $3;

-- name: ListMetadataProjectionStatesByEntities :many
SELECT *
FROM metadata_projection_states
WHERE entity_id = ANY(sqlc.arg(entity_ids)::uuid[])
ORDER BY entity_id, local_kind, local_id, scope;

-- name: ListMetadataScopeTargetsByEntities :many
-- Scope workers write directly to the locally bound row. This deliberately
-- differs from ListMetadataChangeTargetsByEntities, which resolves a child
-- binding to the parent media_item used by a full-document enrichment.
SELECT local_kind, local_id, entity_id, entity_kind, projection_version
FROM metadata_entity_bindings
WHERE entity_id = ANY(sqlc.arg(entity_ids)::uuid[])
ORDER BY entity_id, local_kind, local_id;

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

-- name: ListMetadataChangeTargetsByEntities :many
-- Resolve a complete change-feed page to refresh targets in one round trip.
-- One canonical author may own multiple local books, hence UNION ALL rather
-- than a scalar CASE. The worker deduplicates final target IDs across pages.
WITH bindings AS (
  SELECT entity_id, local_kind, local_id, projection_version
  FROM metadata_entity_bindings
  WHERE entity_id = ANY(sqlc.arg(entity_ids)::uuid[])
)
SELECT entity_id, projection_version, 'person'::text AS target_kind, local_id AS target_id
FROM bindings
WHERE local_kind = 'person'
UNION ALL
SELECT entity_id, projection_version, 'media_item', local_id
FROM bindings
WHERE local_kind = 'media_item'
UNION ALL
SELECT binding.entity_id, binding.projection_version, 'media_item', artist.media_item_id
FROM bindings binding JOIN artists artist ON binding.local_kind = 'artist' AND artist.id = binding.local_id
UNION ALL
SELECT binding.entity_id, binding.projection_version, 'media_item', artist.media_item_id
FROM bindings binding
JOIN albums album ON binding.local_kind = 'album' AND album.id = binding.local_id
JOIN artists artist ON artist.id = album.artist_id
UNION ALL
SELECT binding.entity_id, binding.projection_version, 'media_item', artist.media_item_id
FROM bindings binding
JOIN tracks track ON binding.local_kind = 'track' AND track.id = binding.local_id
JOIN albums album ON album.id = track.album_id
JOIN artists artist ON artist.id = album.artist_id
UNION ALL
SELECT binding.entity_id, binding.projection_version, 'media_item', series.media_item_id
FROM bindings binding
JOIN tv_seasons season ON binding.local_kind = 'tv_season' AND season.id = binding.local_id
JOIN tv_series series ON series.id = season.series_id
UNION ALL
SELECT binding.entity_id, binding.projection_version, 'media_item', series.media_item_id
FROM bindings binding
JOIN tv_episodes episode ON binding.local_kind = 'tv_episode' AND episode.id = binding.local_id
JOIN tv_seasons season ON season.id = episode.season_id
JOIN tv_series series ON series.id = season.series_id
UNION ALL
SELECT binding.entity_id, binding.projection_version, 'media_item', book.media_item_id
FROM bindings binding
JOIN books book ON binding.local_kind = 'author' AND book.author_id = binding.local_id
ORDER BY entity_id, target_kind, target_id;

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
SELECT next_cursor, stream_id
FROM metadata_change_consumers
WHERE consumer = $1;

-- name: CommitMetadataChangeCursor :exec
INSERT INTO metadata_change_consumers (consumer, next_cursor, stream_id)
VALUES ($1, $2, $3)
ON CONFLICT (consumer) DO UPDATE SET
  next_cursor = CASE
    WHEN metadata_change_consumers.stream_id IS NOT DISTINCT FROM EXCLUDED.stream_id
      THEN GREATEST(metadata_change_consumers.next_cursor, EXCLUDED.next_cursor)
    ELSE EXCLUDED.next_cursor
  END,
  stream_id = EXCLUDED.stream_id,
  updated_at = now();

-- name: ResetMetadataChangeCursor :exec
INSERT INTO metadata_change_consumers (consumer, next_cursor, stream_id)
VALUES ($1, 0, $2)
ON CONFLICT (consumer) DO UPDATE SET
  next_cursor = 0,
  stream_id = EXCLUDED.stream_id,
  updated_at = now();
