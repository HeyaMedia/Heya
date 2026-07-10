-- +goose Up

-- Content-hash staleness for embeddings: each facet row remembers a hash of
-- the exact doc text it embedded. The incremental backfill recomposes docs
-- and re-embeds on mismatch, so metadata changes (refresh, re-identify,
-- edited overviews) self-heal on the next pump instead of waiting for a
-- manual force re-embed or an embedder_version bump.
ALTER TABLE public.media_item_facets ADD COLUMN IF NOT EXISTS doc_hash text NOT NULL DEFAULT '';
ALTER TABLE public.episode_facets    ADD COLUMN IF NOT EXISTS doc_hash text NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE public.media_item_facets DROP COLUMN IF EXISTS doc_hash;
ALTER TABLE public.episode_facets    DROP COLUMN IF EXISTS doc_hash;
