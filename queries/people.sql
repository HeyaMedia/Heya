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

-- name: ListPeopleByCanonicalEntityIDs :many
-- Canonical Heya IDs are the strongest identity available. Resolve every
-- person in one round trip before falling back to provider-ID probes. Old
-- databases may contain duplicate canonical person bindings, so choose the
-- oldest local row deterministically until the merge/backfill has folded them.
SELECT DISTINCT ON (binding.entity_id)
       binding.entity_id, person.id, person.name
FROM metadata_entity_bindings binding
JOIN people person
  ON binding.local_kind = 'person' AND person.id = binding.local_id
WHERE binding.entity_id = ANY(sqlc.arg(entity_ids)::uuid[])
ORDER BY binding.entity_id, person.id;

-- name: ListPeopleByExternalIdentifierProbes :many
-- Resolve all fallback provider identifiers in one indexed query. Priority is
-- the stable provider-key order used by the legacy resolver, so ambiguous old
-- rows retain deterministic behaviour while avoiding one round trip per ID.
WITH input AS MATERIALIZED (
    SELECT COALESCE(value->>'identity_key', '') AS identity_key,
           COALESCE((value->>'priority')::integer, 0) AS priority,
           COALESCE(value->>'provider', '') AS provider,
           COALESCE(value->>'provider_id', '') AS provider_id
    FROM jsonb_array_elements(sqlc.arg(probes)::jsonb) AS value
)
SELECT DISTINCT ON (input.identity_key)
       input.identity_key::text AS identity_key, person.id, person.name
FROM input
JOIN people person
  ON person.external_ids @> jsonb_build_object(input.provider, input.provider_id)
WHERE input.provider <> '' AND input.provider_id <> ''
ORDER BY input.identity_key, input.priority, person.id;

-- name: CreatePeopleBulk :many
-- Reserve generated identity values in the materialized input so RETURNING can
-- be joined back to the caller's opaque identity key. This keeps first-time
-- ingestion set-based even when a large title introduces thousands of people.
WITH input AS MATERIALIZED (
    SELECT COALESCE(value->>'identity_key', '') AS identity_key,
           nextval(pg_get_serial_sequence('people', 'id'))::bigint AS id,
           CASE
             WHEN jsonb_typeof(value->'external_ids') = 'object' THEN value->'external_ids'
             ELSE '{}'::jsonb
           END AS external_ids,
           COALESCE(value->>'name', '') AS name,
           COALESCE((value->>'gender')::integer, 0) AS gender,
           COALESCE(value->>'profile_path', '') AS profile_path,
           COALESCE((value->>'popularity')::numeric, 0) AS popularity
    FROM jsonb_array_elements(sqlc.arg(people)::jsonb) AS value
), inserted AS (
    INSERT INTO people (
        id, external_ids, name, also_known_as, gender, profile_path, popularity
    ) OVERRIDING SYSTEM VALUE
    SELECT id, external_ids, name, '{}'::text[], gender, profile_path, popularity
    FROM input
    RETURNING id, name
)
SELECT input.identity_key::text AS identity_key, inserted.id, inserted.name
FROM inserted
JOIN input USING (id)
ORDER BY input.identity_key;

-- name: ReplaceMediaPersonCredits :one
-- Replace the complete cast/crew projection with two set-based inserts. The
-- deletions and inserts share one statement and are ordered through the
-- deletion_counts dependency, avoiding both partial projections and one SQL
-- round trip per credit.
WITH input AS MATERIALIZED (
    SELECT (value->>'person_id')::bigint AS person_id,
           COALESCE((value->>'is_cast')::boolean, false) AS is_cast,
           COALESCE(value->>'character', '') AS character,
           COALESCE((value->>'display_order')::integer, 0) AS display_order,
           COALESCE((value->>'gender')::integer, 0) AS gender,
           COALESCE(value->>'source', '') AS source,
           COALESCE(value->>'job', '') AS job,
           COALESCE(value->>'department', '') AS department
    FROM jsonb_array_elements(sqlc.arg(credits)::jsonb) AS value
), deleted_cast AS (
    DELETE FROM media_cast WHERE media_cast.media_item_id = sqlc.arg(target_media_item_id)
    RETURNING 1
), deleted_crew AS (
    DELETE FROM media_crew WHERE media_crew.media_item_id = sqlc.arg(target_media_item_id)
    RETURNING 1
), deletion_counts AS MATERIALIZED (
    SELECT (SELECT count(*) FROM deleted_cast) AS cast_count,
           (SELECT count(*) FROM deleted_crew) AS crew_count
), inserted_cast AS (
    INSERT INTO media_cast (
        media_item_id, person_id, character, display_order, gender, source
    )
    SELECT sqlc.arg(target_media_item_id), input.person_id, input.character,
           input.display_order, input.gender, input.source
    FROM input CROSS JOIN deletion_counts
    WHERE input.is_cast
    ON CONFLICT (media_item_id, person_id, character) DO NOTHING
    RETURNING 1
), inserted_crew AS (
    INSERT INTO media_crew (
        media_item_id, person_id, job, department, gender, source
    )
    SELECT sqlc.arg(target_media_item_id), input.person_id, input.job,
           input.department, input.gender, input.source
    FROM input CROSS JOIN deletion_counts
    WHERE NOT input.is_cast
    ON CONFLICT (media_item_id, person_id, job) DO NOTHING
    RETURNING 1
)
SELECT (SELECT count(*) FROM inserted_cast)::bigint AS cast_count,
       (SELECT count(*) FROM inserted_crew)::bigint AS crew_count;

-- name: GetPersonBySlug :one
-- The slug indexes are partial (WHERE slug <> ''), and a cached generic plan
-- can't prove an unknown $1 is non-empty — without the explicit predicate the
-- planner falls back to scanning all of people on every lookup.
SELECT * FROM people WHERE slug = $1 AND slug <> '';

-- name: UpdatePersonSlug :exec
UPDATE people SET slug = $2 WHERE id = $1;

-- name: PersonSlugExists :one
-- slug <> '' matches the partial index predicate; see GetPersonBySlug.
SELECT EXISTS(SELECT 1 FROM people WHERE slug = $1 AND slug <> '' AND id != $2) as exists;

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
-- heya_slug <> '' matches the partial index predicate (see GetPersonBySlug) and
-- keeps an empty probe from matching an arbitrary not-yet-enriched row.
SELECT * FROM people WHERE heya_slug = $1 AND heya_slug <> '';

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
