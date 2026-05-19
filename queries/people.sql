-- name: GetPersonByTmdbID :one
SELECT * FROM people WHERE tmdb_id = $1;

-- name: CreatePerson :one
INSERT INTO people (tmdb_id, name, also_known_as, biography, birthday, deathday, place_of_birth, gender, profile_path, homepage, imdb_id, popularity)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (tmdb_id) DO UPDATE SET
  name = EXCLUDED.name,
  also_known_as = EXCLUDED.also_known_as,
  biography = EXCLUDED.biography,
  birthday = EXCLUDED.birthday,
  deathday = EXCLUDED.deathday,
  place_of_birth = EXCLUDED.place_of_birth,
  gender = EXCLUDED.gender,
  profile_path = EXCLUDED.profile_path,
  homepage = EXCLUDED.homepage,
  imdb_id = EXCLUDED.imdb_id,
  popularity = EXCLUDED.popularity,
  updated_at = now()
RETURNING *;

-- name: CreateMediaCast :exec
INSERT INTO media_cast (media_item_id, person_id, character, display_order)
VALUES ($1, $2, $3, $4)
ON CONFLICT (media_item_id, person_id, character) DO NOTHING;

-- name: CreateMediaCrew :exec
INSERT INTO media_crew (media_item_id, person_id, job, department)
VALUES ($1, $2, $3, $4)
ON CONFLICT (media_item_id, person_id, job) DO NOTHING;

-- name: ListMediaCast :many
SELECT mc.character, mc.display_order, p.*
FROM media_cast mc
JOIN people p ON p.id = mc.person_id
WHERE mc.media_item_id = $1
ORDER BY mc.display_order;

-- name: ListMediaCrew :many
SELECT mc.job, mc.department, p.*
FROM media_crew mc
JOIN people p ON p.id = mc.person_id
WHERE mc.media_item_id = $1
ORDER BY mc.department, mc.job;
