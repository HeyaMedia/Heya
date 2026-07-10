-- +goose Up
-- scan_run_artifacts became a write-only ghost table: the queue pipeline stopped
-- writing it (OmitResultArtifacts) and nothing ever read it back — the live
-- resume path uses scanner_entity_artifacts. It only ballooned (5.9GB in TOAST on
-- prod) from the brief window the queue did write full Result blobs into it. All
-- code references are gone; drop the table.
DROP TABLE IF EXISTS public.scan_run_artifacts;

-- +goose Down
CREATE TABLE IF NOT EXISTS public.scan_run_artifacts (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    scan_run_id bigint NOT NULL,
    kind text NOT NULL,
    scope_key text DEFAULT ''::text NOT NULL,
    schema_version integer DEFAULT 1 NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT scan_run_artifacts_pkey PRIMARY KEY (id),
    CONSTRAINT scan_run_artifacts_scan_run_id_kind_scope_key_key UNIQUE (scan_run_id, kind, scope_key),
    CONSTRAINT scan_run_artifacts_scan_run_id_fkey FOREIGN KEY (scan_run_id) REFERENCES public.scan_runs(id) ON DELETE CASCADE
);
