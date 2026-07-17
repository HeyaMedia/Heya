-- +goose Up

-- Remote metadata workflows can remain pending for a long time. Keeping one
-- scheduled river_job per workflow makes River's hot table (and every piece
-- of queue telemetry around it) grow with the remote provider's backlog.
-- Park those compact continuations here; a bounded periodic worker promotes
-- only a small due batch back into River.
CREATE TABLE public.scanner_metadata_continuations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('search_metadata', 'fetch_metadata')),
    library_id bigint NOT NULL REFERENCES public.libraries(id) ON DELETE CASCADE,
    scanner_entity_id bigint NOT NULL REFERENCES public.scanner_entities(id) ON DELETE CASCADE,
    artifact_id bigint NOT NULL REFERENCES public.scanner_entity_artifacts(id) ON DELETE CASCADE,
    args jsonb NOT NULL,
    priority smallint NOT NULL DEFAULT 2 CHECK (priority BETWEEN 1 AND 4),
    source text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, scanner_entity_id, artifact_id)
);

CREATE INDEX scanner_metadata_continuations_due_idx
    ON public.scanner_metadata_continuations (next_attempt_at, id);

CREATE INDEX scanner_metadata_continuations_library_idx
    ON public.scanner_metadata_continuations (library_id, kind);

CREATE INDEX scanner_metadata_continuations_entity_idx
    ON public.scanner_metadata_continuations (scanner_entity_id);

CREATE INDEX scanner_metadata_continuations_artifact_idx
    ON public.scanner_metadata_continuations (artifact_id);

-- +goose Down

DROP TABLE IF EXISTS public.scanner_metadata_continuations;
