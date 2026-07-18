-- +goose Up

-- Canonical entities contain independently refreshed projections (for
-- example an artist's top_tracks). The entity binding's projection_version
-- says that the parent document was seen, but it cannot prove that every
-- separately fetched child projection was applied successfully. Keep a
-- durable checkpoint per local target + scope so failed/empty child fetches
-- remain distinguishable and the change feed can retry only the missing part.
CREATE TABLE public.metadata_projection_states (
    local_kind text NOT NULL,
    local_id bigint NOT NULL,
    scope text NOT NULL,
    entity_id uuid NOT NULL,
    entity_kind text NOT NULL,
    projection_version bigint NOT NULL DEFAULT 0,
    applied_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT metadata_projection_states_pkey PRIMARY KEY (local_kind, local_id, scope),
    CONSTRAINT metadata_projection_states_binding_fkey
        FOREIGN KEY (local_kind, local_id)
        REFERENCES public.metadata_entity_bindings (local_kind, local_id)
        ON DELETE CASCADE,
    CONSTRAINT metadata_projection_states_scope_check CHECK (scope <> ''),
    CONSTRAINT metadata_projection_states_projection_version_check CHECK (projection_version >= 0)
);

CREATE INDEX metadata_projection_states_entity_scope_idx
    ON public.metadata_projection_states (entity_id, scope, projection_version);

-- +goose Down

DROP TABLE public.metadata_projection_states;
