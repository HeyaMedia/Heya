-- name: CreatePersonProfile :exec
INSERT INTO person_profiles (person_id, url, source, aspect, width, height, score, sort_order)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (person_id, url) DO UPDATE SET
  source = EXCLUDED.source, width = EXCLUDED.width, height = EXCLUDED.height,
  score = EXCLUDED.score, sort_order = EXCLUDED.sort_order;

-- name: ListPersonProfiles :many
SELECT * FROM person_profiles WHERE person_id = $1 ORDER BY score DESC, sort_order;

-- name: DeletePersonProfiles :exec
DELETE FROM person_profiles WHERE person_id = $1;
