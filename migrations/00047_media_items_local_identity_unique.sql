-- +goose Up
-- Enforce natural-identity uniqueness among UN-ENRICHED local stubs
-- (enrichment_status='local') so concurrent metadata_match workers can't fork
-- the same NFO-less show into two rows. Complements idx_media_items_identity
-- (00044, non-unique, the dedup probe's index) with a hard constraint on the
-- local-stub subset that the create-time retry-on-conflict in the matcher leans
-- on to serialize the race.
--
-- Scoped to enrichment_status='local' on purpose: enrich moves an item to
-- 'complete', which drops it OUT of this index — so re-identified / enriched
-- local-origin items are never constrained here (their title/year updates can't
-- trip it) and the self-healing dedup below never touches their real data. A
-- local stub and a same-identity matched/enriched item still coexist; the
-- fold-vs-fork call stays the matcher's (foldIntoMatched). The local path stamps
-- 'local' AT INSERT (CreateMediaItem), so the conflict fires at create.

-- Self-healing: merge any pre-existing duplicate local stubs before adding the
-- constraint (a no-op where there are none — prod has zero enrichment_status=
-- 'local' rows today). Keep the oldest per identity, reparent its library_files
-- to it, then delete the losers (their other children cascade). library_files is
-- ON DELETE SET NULL, so it MUST be reparented before the delete or the file
-- would lose its match.
UPDATE library_files lf
SET media_item_id = keep.keep_id
FROM (
    SELECT id,
           min(id) OVER (PARTITION BY library_id, media_type, year, lower(btrim(title))) AS keep_id
    FROM media_items
    WHERE enrichment_status = 'local'
) keep
WHERE lf.media_item_id = keep.id
  AND keep.id <> keep.keep_id;

DELETE FROM media_items mi
USING (
    SELECT id,
           min(id) OVER (PARTITION BY library_id, media_type, year, lower(btrim(title))) AS keep_id
    FROM media_items
    WHERE enrichment_status = 'local'
) keep
WHERE mi.id = keep.id
  AND keep.id <> keep.keep_id;

CREATE UNIQUE INDEX idx_media_items_local_identity
    ON media_items (library_id, media_type, year, lower(btrim(title)))
    WHERE enrichment_status = 'local';

-- +goose Down
DROP INDEX IF EXISTS idx_media_items_local_identity;
