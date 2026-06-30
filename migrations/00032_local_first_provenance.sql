-- +goose Up

-- Local-first ingest provenance. Entities can now be materialized from local
-- signal (NFO / embedded tags / filename) BEFORE any remote match, then upgraded
-- in place when enrichment lands. These columns capture the two axes the
-- existing enrichment_status lifecycle doesn't:
--
--   field_provenance   – per-field source map {field: "local"|"remote"|"user"}.
--                        GROUNDWORK for Phase-2 edits-win: once the metadata
--                        editor stamps "user" and the enrich writers consult it,
--                        manual edits will be protected. NOT yet enforced — in
--                        Phase 0 the type-specific writes are get-or-create
--                        (insert when absent, preserve the whole row on conflict),
--                        so nothing is overwritten regardless of this column.
--   match_confidence   – search-stub fast-path score (0 = pure-local, never
--                        confidently matched). Also informs dedup.
--   local_identity_key – normalized dedup key for NFO-less locals
--                        (lower(title)|year|media_type, or canonical folder path)
--                        so re-scans link to the same entity instead of relying
--                        on external_ids containment (which mis-joins on '{}').
--   slug_locked        – the user-facing slug is frozen (set at first publish or
--                        on any user edit); re-enrich must never change it.
--
-- enrichment_status (TEXT, migration 00017) gains one new value 'local' for
-- "born from local signal, never confidently matched" — no schema change needed,
-- and the existing idx_media_items_enrichment_pending partial index
-- (WHERE enrichment_status != 'complete') already covers it.
ALTER TABLE media_items
    ADD COLUMN field_provenance   JSONB    NOT NULL DEFAULT '{}',
    ADD COLUMN match_confidence   REAL     NOT NULL DEFAULT 0,
    ADD COLUMN local_identity_key TEXT     NOT NULL DEFAULT '',
    ADD COLUMN slug_locked        BOOLEAN  NOT NULL DEFAULT false;

-- Dedup lookup for locally-materialized entities. Non-unique for now: Phase 1
-- decides whether to promote to a unique (library_id, local_identity_key)
-- constraint once the resolver's key normalization is settled.
CREATE INDEX idx_media_items_local_identity_key
    ON media_items (library_id, local_identity_key)
    WHERE local_identity_key != '';

-- +goose Down

DROP INDEX IF EXISTS idx_media_items_local_identity_key;
ALTER TABLE media_items
    DROP COLUMN slug_locked,
    DROP COLUMN local_identity_key,
    DROP COLUMN match_confidence,
    DROP COLUMN field_provenance;
