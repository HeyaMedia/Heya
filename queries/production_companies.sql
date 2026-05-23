-- name: FindProductionCompanyByName :one
SELECT * FROM production_companies WHERE name = $1;

-- name: FindProductionCompanyByExternalID :one
SELECT * FROM production_companies WHERE external_ids @> $1::jsonb LIMIT 1;

-- name: CreateProductionCompany :one
INSERT INTO production_companies (external_ids, name, logo_path, origin_country)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: LinkMediaProductionCompany :exec
INSERT INTO media_production_companies (media_item_id, company_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListAllProductionCompanies :many
SELECT * FROM production_companies ORDER BY name;

-- name: GetProductionCompanyByID :one
SELECT * FROM production_companies WHERE id = $1;

-- name: ListMediaProductionCompanies :many
SELECT pc.* FROM production_companies pc
JOIN media_production_companies mpc ON mpc.company_id = pc.id
WHERE mpc.media_item_id = $1
ORDER BY pc.name;

-- name: SearchProductionCompaniesByName :many
SELECT pc.id, pc.name, pc.logo_path
FROM production_companies pc
WHERE lower(pc.name) LIKE lower(@query) || '%'
ORDER BY pc.name
LIMIT @max_results;

-- name: DeleteMediaProductionCompaniesByItem :exec
DELETE FROM media_production_companies WHERE media_item_id = $1;

-- name: ListStudioMediaItemIDs :many
SELECT DISTINCT mpc.media_item_id
FROM media_production_companies mpc
WHERE mpc.company_id = ANY(@company_ids::bigint[]);
