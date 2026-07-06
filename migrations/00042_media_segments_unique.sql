-- +goose Up

-- One non-commercial segment row per (file, type), enforced at the DB
-- level. The community worker (scan_media_segments_file) and the local
-- chromaprint detector (detect_segments_season) run concurrently on
-- different queues, and both guard their inserts with read-committed
-- EXISTS checks — each can see "no other-source row yet" and both insert,
-- leaving a file with e.g. a community:* intro AND a chromaprint intro.
-- The unique index is the backstop; the writers insert with ON CONFLICT
-- DO NOTHING so losing the race silently keeps the first-arriver, which
-- is exactly the peers-by-arrival-order policy. commercial is excluded:
-- multiple commercial breaks per file are legitimate.

-- Defensive dedupe of any existing violations before the index lands:
-- keep exactly one row per (file, type) by precedence manual >
-- chromaprint > community:% > blackframe, ties broken by lowest id.
DELETE FROM media_segments ms
USING (
    SELECT id,
           row_number() OVER (
               PARTITION BY library_file_id, segment_type
               ORDER BY
                   CASE
                       WHEN source = 'manual' THEN 0
                       WHEN source = 'chromaprint' THEN 1
                       WHEN source LIKE 'community:%' THEN 2
                       ELSE 3
                   END,
                   id
           ) AS rn
    FROM media_segments
    WHERE segment_type <> 'commercial'
) ranked
WHERE ms.id = ranked.id
  AND ranked.rn > 1;

CREATE UNIQUE INDEX idx_media_segments_file_type
    ON media_segments (library_file_id, segment_type)
    WHERE segment_type <> 'commercial';

-- +goose Down
DROP INDEX idx_media_segments_file_type;
