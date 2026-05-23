-- name: UpsertNetworkByExternalIDs :one
INSERT INTO networks (name, external_ids, logo_path, country)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name) DO UPDATE SET
  external_ids = networks.external_ids || EXCLUDED.external_ids,
  logo_path = CASE WHEN networks.logo_path = '' THEN EXCLUDED.logo_path ELSE networks.logo_path END,
  country = CASE WHEN networks.country = '' THEN EXCLUDED.country ELSE networks.country END
RETURNING *;

-- name: FindNetworkByExternalID :one
SELECT * FROM networks WHERE external_ids @> $1::jsonb LIMIT 1;

-- name: AttachNetworkToSeries :exec
INSERT INTO tv_series_networks (series_id, network_id, sort_order)
VALUES ($1, $2, $3)
ON CONFLICT (series_id, network_id) DO UPDATE SET sort_order = EXCLUDED.sort_order;

-- name: DeleteNetworksForSeries :exec
DELETE FROM tv_series_networks WHERE series_id = $1;

-- name: ListNetworksForSeries :many
SELECT n.* FROM networks n
JOIN tv_series_networks tsn ON tsn.network_id = n.id
WHERE tsn.series_id = $1
ORDER BY tsn.sort_order;

-- name: UpsertCreatorByExternalIDs :one
INSERT INTO creators (name, external_ids)
VALUES ($1, $2)
ON CONFLICT (name) DO UPDATE SET
  external_ids = creators.external_ids || EXCLUDED.external_ids
RETURNING *;

-- name: FindCreatorByExternalID :one
SELECT * FROM creators WHERE external_ids @> $1::jsonb LIMIT 1;

-- name: AttachCreatorToSeries :exec
INSERT INTO tv_series_creators (series_id, creator_id, sort_order)
VALUES ($1, $2, $3)
ON CONFLICT (series_id, creator_id) DO UPDATE SET sort_order = EXCLUDED.sort_order;

-- name: DeleteCreatorsForSeries :exec
DELETE FROM tv_series_creators WHERE series_id = $1;

-- name: ListCreatorsForSeries :many
SELECT c.* FROM creators c
JOIN tv_series_creators tsc ON tsc.creator_id = c.id
WHERE tsc.series_id = $1
ORDER BY tsc.sort_order;
