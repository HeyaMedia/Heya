-- name: CreateMediaTitle :exec
INSERT INTO media_titles (media_item_id, title, language, country, title_type, source)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (media_item_id, title, language) DO UPDATE SET
  country = EXCLUDED.country, title_type = EXCLUDED.title_type, source = EXCLUDED.source;

-- name: ListMediaTitles :many
SELECT * FROM media_titles WHERE media_item_id = $1 ORDER BY language, title_type;

-- name: GetMediaTitleByLanguage :one
-- Pick the best matching title for a (item, language) pair:
--   * Prefer an exact language match (en = en) over a locale-variant
--     match (en matches en-US / en-GB) — handled in the ORDER BY.
--   * Prefer official > original > romanized > alternative > anything —
--     heya.media tags anime English titles as 'alternative' / 'romanized'
--     rather than 'official', so a strict title_type='official' filter
--     would miss them entirely and the UI would fall back to the raw
--     canonical Japanese title.
SELECT * FROM media_titles
WHERE media_item_id = $1
  AND (language = $2 OR language LIKE $2 || '-%')
ORDER BY
  CASE WHEN language = $2 THEN 0 ELSE 1 END,
  CASE title_type
    WHEN 'official' THEN 0
    WHEN 'original' THEN 1
    WHEN 'romanized' THEN 2
    WHEN 'alternative' THEN 3
    ELSE 4
  END,
  id
LIMIT 1;

-- name: GetMediaTitlesByLanguageBatch :many
-- Batched GetMediaTitleByLanguage for the list endpoints: one query per page
-- of items instead of one per item (the home rails paid ~60 sequential
-- round trips per load). DISTINCT ON keeps exactly the row the single-item
-- ORDER BY would have picked for each item.
SELECT DISTINCT ON (media_item_id) *
FROM media_titles
WHERE media_item_id = ANY(sqlc.arg(media_item_ids)::bigint[])
  AND (language = sqlc.arg(language) OR language LIKE sqlc.arg(language) || '-%')
ORDER BY
  media_item_id,
  CASE WHEN language = sqlc.arg(language) THEN 0 ELSE 1 END,
  CASE title_type
    WHEN 'official' THEN 0
    WHEN 'original' THEN 1
    WHEN 'romanized' THEN 2
    WHEN 'alternative' THEN 3
    ELSE 4
  END,
  id;

-- name: DeleteMediaTitlesByItem :exec
DELETE FROM media_titles WHERE media_item_id = $1;
