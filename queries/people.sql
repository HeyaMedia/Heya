-- name: GetPersonByExternalID :one
SELECT * FROM people WHERE external_ids @> $1::jsonb LIMIT 1;

-- name: FindPersonByExternalID :one
SELECT * FROM people WHERE external_ids @> $1::jsonb LIMIT 1;

-- name: CreatePerson :one
INSERT INTO people (external_ids, name, also_known_as, biography, birthday, deathday, place_of_birth, gender, profile_path, homepage, popularity, sort_name, known_for_department, birth_year, heya_slug)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: CreateMediaCast :exec
INSERT INTO media_cast (media_item_id, person_id, character, display_order, gender, source)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (media_item_id, person_id, character) DO NOTHING;

-- name: CreateMediaCrew :exec
INSERT INTO media_crew (media_item_id, person_id, job, department, gender, source)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (media_item_id, person_id, job) DO NOTHING;

-- name: ListMediaCast :many
SELECT mc.character, mc.display_order, p.*
FROM media_cast mc
JOIN people p ON p.id = mc.person_id
WHERE mc.media_item_id = $1
ORDER BY mc.display_order;

-- name: ListMediaCastSlim :many
SELECT mc.character, mc.display_order, p.id, p.name, p.profile_path, p.gender
FROM media_cast mc
JOIN people p ON p.id = mc.person_id
WHERE mc.media_item_id = $1
ORDER BY mc.display_order;

-- name: ListMediaCrewSlim :many
SELECT mc.job, mc.department, p.id, p.name, p.profile_path
FROM media_crew mc
JOIN people p ON p.id = mc.person_id
WHERE mc.media_item_id = $1
ORDER BY mc.department, mc.job;

-- name: UpdatePersonProfilePath :exec
UPDATE people SET profile_path = $2, updated_at = now() WHERE id = $1;

-- name: GetPersonByID :one
SELECT * FROM people WHERE id = $1;

-- name: LockPeopleForCreditWrite :many
-- Keep resolved people alive while an authoritative cast/crew relationship set
-- is replaced. Compatible across concurrent title applies, but conflicts with
-- the exclusive merge lock that can delete a duplicate person.
SELECT id
FROM people
WHERE id = ANY(@person_ids::bigint[])
ORDER BY id
FOR KEY SHARE;

-- name: LockPeopleForMerge :many
-- Person merges acquire the same IDs in the same order before touching credit
-- rows, preventing a merge and title apply from locking people/credits in the
-- opposite order.
SELECT id
FROM people
WHERE id = ANY(@person_ids::bigint[])
ORDER BY id
FOR UPDATE;

-- name: GetPersonBySlug :one
SELECT * FROM people WHERE slug = $1;

-- name: UpdatePersonSlug :exec
UPDATE people SET slug = $2 WHERE id = $1;

-- name: PersonSlugExists :one
SELECT EXISTS(SELECT 1 FROM people WHERE slug = $1 AND id != $2) as exists;

-- name: ListMediaCrew :many
SELECT mc.job, mc.department, p.*
FROM media_crew mc
JOIN people p ON p.id = mc.person_id
WHERE mc.media_item_id = $1
ORDER BY mc.department, mc.job;

-- name: ListPersonCastCredits :many
SELECT mc.character, mc.display_order, mi.id as media_item_id, mi.public_id AS media_item_public_id, mi.title, mi.year, mi.media_type, mi.poster_path
FROM media_cast mc
JOIN media_item_cards mi ON mi.id = mc.media_item_id
WHERE mc.person_id = $1
ORDER BY mi.year DESC, mi.title;

-- name: ListPersonCrewCredits :many
SELECT mc.job, mc.department, mi.id as media_item_id, mi.public_id AS media_item_public_id, mi.title, mi.year, mi.media_type, mi.poster_path
FROM media_crew mc
JOIN media_item_cards mi ON mi.id = mc.media_item_id
WHERE mc.person_id = $1
ORDER BY mi.year DESC, mi.title;

-- name: SearchPeopleByName :many
SELECT p.id, p.name, p.profile_path
FROM people p
WHERE lower(p.name) LIKE lower(@query) || '%'
ORDER BY p.popularity DESC NULLS LAST
LIMIT @max_results;

-- name: DeleteMediaCastByItem :exec
DELETE FROM media_cast WHERE media_item_id = $1;

-- name: DeleteMediaCrewByItem :exec
DELETE FROM media_crew WHERE media_item_id = $1;

-- name: ListCastMediaItemIDs :many
SELECT DISTINCT mc.media_item_id
FROM media_cast mc
WHERE mc.person_id = ANY(@person_ids::bigint[]);

-- name: ListCrewMediaItemIDs :many
SELECT DISTINCT mc.media_item_id
FROM media_crew mc
WHERE mc.person_id = ANY(@person_ids::bigint[]);

-- name: GetPersonByHeyaSlug :one
SELECT * FROM people WHERE heya_slug = $1;

-- name: UpdatePersonFull :one
UPDATE people SET
  name = $2, also_known_as = $3, biography = $4, birthday = $5, deathday = $6,
  place_of_birth = $7, gender = $8, profile_path = $9, homepage = $10,
  popularity = $11, external_ids = $12, sort_name = $13, known_for_department = $14,
  birth_year = $15, heya_slug = $16, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkPersonEnriched :exec
UPDATE people SET heya_enriched_at = now() WHERE id = $1;

-- Person merge ----------------------------------------------------------------
-- Two person rows can resolve to the same upstream person (the same actor
-- scanned under name variants across titles). They share an idx_people_heya_slug
-- collision; the fix is to fold src into the canonical dst. people's derived
-- children (biographies, profiles, external_credits) are all ON DELETE CASCADE
-- and regenerate from dst's own enrichment, so only the credit *links*
-- (media_cast / media_crew) need to survive the merge. People are neither rated
-- nor favorited, so there's no user data to migrate.

-- name: DeleteCollidingPersonCast :exec
-- Drop src cast rows that would collide with dst on (media_item_id, character)
-- before the reparent UPDATE moves the rest.
DELETE FROM media_cast src
WHERE src.person_id = sqlc.arg(src_id)
  AND EXISTS (
      SELECT 1 FROM media_cast dst
      WHERE dst.person_id = sqlc.arg(dst_id)
        AND dst.media_item_id = src.media_item_id
        AND dst.character = src.character
  );

-- name: ReparentPersonCast :exec
UPDATE media_cast SET person_id = sqlc.arg(dst_id) WHERE person_id = sqlc.arg(src_id);

-- name: DeleteCollidingPersonCrew :exec
-- Drop src crew rows that would collide with dst on (media_item_id, job)
-- before the reparent UPDATE moves the rest.
DELETE FROM media_crew src
WHERE src.person_id = sqlc.arg(src_id)
  AND EXISTS (
      SELECT 1 FROM media_crew dst
      WHERE dst.person_id = sqlc.arg(dst_id)
        AND dst.media_item_id = src.media_item_id
        AND dst.job = src.job
  );

-- name: ReparentPersonCrew :exec
UPDATE media_crew SET person_id = sqlc.arg(dst_id) WHERE person_id = sqlc.arg(src_id);

-- name: DeletePerson :exec
-- Remove the emptied src person. CASCADE clears its biographies, profiles, and
-- external_credits (and any cast/crew not already reparented).
DELETE FROM people WHERE id = sqlc.arg(id);
