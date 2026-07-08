-- +goose Up

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

CREATE INDEX IF NOT EXISTS idx_scan_run_artifacts_kind_scope
    ON public.scan_run_artifacts USING btree (kind, scope_key, scan_run_id);

-- +goose Down

DROP TABLE IF EXISTS public.scan_run_artifacts;
