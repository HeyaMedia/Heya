-- name: CreateProductionCompany :one
INSERT INTO production_companies (tmdb_id, name, logo_path, origin_country)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tmdb_id) DO UPDATE SET name = EXCLUDED.name, logo_path = EXCLUDED.logo_path, origin_country = EXCLUDED.origin_country
RETURNING *;

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
