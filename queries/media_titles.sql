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

-- name: DeleteMediaTitlesByItem :exec
DELETE FROM media_titles WHERE media_item_id = $1;
