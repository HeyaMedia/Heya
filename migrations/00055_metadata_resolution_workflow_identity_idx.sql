-- +goose Up

-- Deleting or replacing a scanner identity fires the workflow foreign key's
-- ON DELETE SET NULL check once per identity. Without an index on identity_id,
-- PostgreSQL scans the entire workflow table for every deleted row even when
-- no workflows currently reference an identity.
CREATE INDEX IF NOT EXISTS idx_metadata_resolution_workflows_identity
    ON public.metadata_resolution_workflows (identity_id)
    WHERE identity_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS public.idx_metadata_resolution_workflows_identity;
