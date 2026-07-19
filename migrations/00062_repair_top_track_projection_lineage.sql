-- +goose Up

-- Versions written before this migration could be promoted from the parent
-- artist binding after the top-tracks HTTP response had already been fetched.
-- Such a checkpoint may claim (for example) v10 while its rows are really the
-- v8 snapshot, causing the genuine v10 scope job to no-op forever.
--
-- Preserve the last known rows while forgetting only the untrustworthy
-- checkpoint. The normal metadata-scope backfill sees the missing checkpoint
-- and atomically replaces/checkpoints the authoritative endpoint snapshot.
-- This DELETE is intentionally idempotent.
DELETE FROM public.metadata_projection_states
WHERE scope = 'top_tracks';

-- +goose Down

-- A deleted lineage checkpoint cannot be reconstructed safely from local
-- rows. Rolling back the binary therefore leaves the rows intact and lets the
-- older reconciler rebuild its own checkpoint on its next pass.
SELECT 1;
