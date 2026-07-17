-- +goose Up

-- Discovery completions are announced by HeyaMetadata's gap-free workflow
-- event feed. Persist the upstream workflow identity on parked scanner work so
-- one feed page can wake every matching continuation without polling each
-- discovery independently.
ALTER TABLE public.scanner_metadata_continuations
    ADD COLUMN workflow_kind text NOT NULL DEFAULT '',
    ADD COLUMN workflow_id uuid,
    ADD COLUMN workflow_event_sequence bigint NOT NULL DEFAULT 0;

ALTER TABLE public.scanner_metadata_continuations
    ADD CONSTRAINT scanner_metadata_continuations_workflow_check CHECK (
        (workflow_kind = '' AND workflow_id IS NULL) OR
        (workflow_kind <> '' AND workflow_id IS NOT NULL)
    );

CREATE INDEX scanner_metadata_continuations_workflow_idx
    ON public.scanner_metadata_continuations (workflow_kind, workflow_id)
    WHERE workflow_id IS NOT NULL;

-- Only submission failures without a durable workflow ID participate in the
-- adaptive polling count. Keep that once-per-minute count on a tiny partial
-- index even when the event-driven parked population is very large.
CREATE INDEX scanner_metadata_continuations_unbound_search_idx
    ON public.scanner_metadata_continuations (id)
    WHERE kind = 'search_metadata' AND workflow_id IS NULL;

CREATE INDEX metadata_resolution_workflows_discovery_idx
    ON public.metadata_resolution_workflows (discovery_id)
    WHERE discovery_id IS NOT NULL;

-- The cursor commits in the same transaction as the local wake-up effects.
CREATE TABLE public.metadata_workflow_event_consumers (
    consumer text PRIMARY KEY,
    next_cursor bigint NOT NULL DEFAULT 0,
    stream_id uuid,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO public.metadata_workflow_event_consumers (consumer)
VALUES ('heya-scanner')
ON CONFLICT (consumer) DO NOTHING;

-- Keep only the newest recognized completion per workflow. This is a narrow
-- race buffer, not a copy of HeyaMetadata's global feed: the consumer inserts
-- rows only for discovery IDs already present in Heya's workflow table.
CREATE TABLE public.metadata_workflow_event_inbox (
    workflow_kind text NOT NULL,
    workflow_id uuid NOT NULL,
    sequence bigint NOT NULL,
    state text NOT NULL,
    completed_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workflow_kind, workflow_id)
);

-- +goose Down

DROP TABLE IF EXISTS public.metadata_workflow_event_inbox;
DROP TABLE IF EXISTS public.metadata_workflow_event_consumers;

DROP INDEX IF EXISTS public.metadata_resolution_workflows_discovery_idx;
DROP INDEX IF EXISTS public.scanner_metadata_continuations_unbound_search_idx;
DROP INDEX IF EXISTS public.scanner_metadata_continuations_workflow_idx;
ALTER TABLE public.scanner_metadata_continuations
    DROP CONSTRAINT IF EXISTS scanner_metadata_continuations_workflow_check,
    DROP COLUMN IF EXISTS workflow_event_sequence,
    DROP COLUMN IF EXISTS workflow_id,
    DROP COLUMN IF EXISTS workflow_kind;
