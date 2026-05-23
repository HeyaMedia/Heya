-- name: CreatePersonBiography :exec
INSERT INTO person_biographies (person_id, language, biography)
VALUES ($1, $2, $3)
ON CONFLICT (person_id, language) DO UPDATE SET biography = EXCLUDED.biography;

-- name: ListPersonBiographies :many
SELECT * FROM person_biographies WHERE person_id = $1 ORDER BY language;

-- name: DeletePersonBiographies :exec
DELETE FROM person_biographies WHERE person_id = $1;
