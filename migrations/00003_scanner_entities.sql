-- +goose Up

CREATE TABLE IF NOT EXISTS public.scanner_entities (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    library_id bigint NOT NULL,
    media_type public.media_type NOT NULL,
    scope_key text DEFAULT ''::text NOT NULL,
    scope_paths text[] DEFAULT '{}'::text[] NOT NULL,
    identity_key text DEFAULT ''::text NOT NULL,
    title text DEFAULT ''::text NOT NULL,
    year text DEFAULT ''::text NOT NULL,
    provider_id text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'discovered'::text NOT NULL,
    search_scan_run_id bigint,
    fetch_scan_run_id bigint,
    search_artifact_id bigint,
    metadata_artifact_id bigint,
    apply_artifact_id bigint,
    error_message text DEFAULT ''::text NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    discovered_at timestamp with time zone DEFAULT now() NOT NULL,
    searched_at timestamp with time zone,
    fetched_at timestamp with time zone,
    applied_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT scanner_entities_pkey PRIMARY KEY (id),
    CONSTRAINT scanner_entities_library_id_fkey FOREIGN KEY (library_id) REFERENCES public.libraries(id) ON DELETE CASCADE,
    CONSTRAINT scanner_entities_scope_identity_key UNIQUE (library_id, media_type, scope_key, identity_key)
);

CREATE INDEX IF NOT EXISTS idx_scanner_entities_library_status
    ON public.scanner_entities USING btree (library_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_scanner_entities_provider
    ON public.scanner_entities USING btree (media_type, provider_id)
    WHERE provider_id <> '';

CREATE TABLE IF NOT EXISTS public.scanner_entity_artifacts (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    entity_id bigint NOT NULL,
    stage text NOT NULL,
    schema_version integer DEFAULT 1 NOT NULL,
    scan_run_id bigint,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT scanner_entity_artifacts_pkey PRIMARY KEY (id),
    CONSTRAINT scanner_entity_artifacts_entity_id_fkey FOREIGN KEY (entity_id) REFERENCES public.scanner_entities(id) ON DELETE CASCADE,
    CONSTRAINT scanner_entity_artifacts_scan_run_id_fkey FOREIGN KEY (scan_run_id) REFERENCES public.scan_runs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_scanner_entity_artifacts_entity_stage
    ON public.scanner_entity_artifacts USING btree (entity_id, stage, id DESC);

-- +goose Down

DROP TABLE IF EXISTS public.scanner_entity_artifacts;
DROP TABLE IF EXISTS public.scanner_entities;
