-- +goose Up

-- Leading articles are semantically meaningful in a durable identity key.
-- The fuzzy matcher may compare them loosely, but collapsing "The Office"
-- and "Office" into the same uniqueness boundary can merge distinct works.
WITH rewritten AS (
    SELECT
        id,
        split_part(identity_key, ':', 1) || ':' ||
            lower(split_part(title, ' ', 1)) || ' ' ||
            substring(identity_key FROM position(':' IN identity_key) + 1) AS new_key
    FROM public.local_media_identities
    WHERE media_type IN ('movie', 'tv', 'anime')
      AND identity_key ~ '^title(_year)?:'
      AND lower(title) ~ '^(the|a|an)[[:space:]]'
      AND substring(identity_key FROM position(':' IN identity_key) + 1)
            NOT LIKE lower(split_part(title, ' ', 1)) || ' %'
)
UPDATE public.local_media_identities AS identity
SET identity_key = rewritten.new_key,
    raw_identity = CASE
        WHEN identity.raw_identity ? 'key'
            THEN jsonb_set(identity.raw_identity, '{key}', to_jsonb(rewritten.new_key), false)
        ELSE identity.raw_identity
    END,
    updated_at = now()
FROM rewritten
WHERE identity.id = rewritten.id
  AND identity.identity_key <> rewritten.new_key;

-- +goose Down

-- Reverse rows only where doing so cannot collide with a distinct articleless
-- identity that was created after this migration.
WITH rewritten AS (
    SELECT
        identity.id,
        split_part(identity.identity_key, ':', 1) || ':' ||
            regexp_replace(
                substring(identity.identity_key FROM position(':' IN identity.identity_key) + 1),
                '^(the|a|an)[[:space:]]+',
                '',
                'i'
            ) AS old_key
    FROM public.local_media_identities AS identity
    WHERE identity.media_type IN ('movie', 'tv', 'anime')
      AND identity.identity_key ~ '^title(_year)?:(the|a|an)[[:space:]]+'
      AND lower(identity.title) ~ '^(the|a|an)[[:space:]]'
)
UPDATE public.local_media_identities AS identity
SET identity_key = rewritten.old_key,
    raw_identity = CASE
        WHEN identity.raw_identity ? 'key'
            THEN jsonb_set(identity.raw_identity, '{key}', to_jsonb(rewritten.old_key), false)
        ELSE identity.raw_identity
    END,
    updated_at = now()
FROM rewritten
WHERE identity.id = rewritten.id
  AND NOT EXISTS (
      SELECT 1
      FROM public.local_media_identities AS conflict
      WHERE conflict.library_id = identity.library_id
        AND conflict.media_type = identity.media_type
        AND conflict.identity_key = rewritten.old_key
        AND conflict.id <> identity.id
  );
