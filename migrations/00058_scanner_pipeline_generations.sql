-- +goose Up

ALTER TABLE public.scanner_entities
    ADD COLUMN analysis_artifact_id bigint,
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT scanner_entities_pipeline_generation_positive
        CHECK (pipeline_generation > 0);

ALTER TABLE public.scanner_entity_artifacts
    ADD COLUMN pipeline_generation bigint NOT NULL DEFAULT 1,
    ADD COLUMN source_artifact_id bigint,
    ADD CONSTRAINT scanner_entity_artifacts_pipeline_generation_positive
        CHECK (pipeline_generation > 0),
    ADD CONSTRAINT scanner_entity_artifacts_source_artifact_id_fkey
        FOREIGN KEY (source_artifact_id)
        REFERENCES public.scanner_entity_artifacts(id)
        ON DELETE SET NULL;

-- Existing rows predate explicit generations. Treat the newest retained local
-- analysis as generation one's current analysis hand-off. Stage lineage for
-- legacy artifacts is intentionally left unknown rather than guessed.
UPDATE public.scanner_entities entity
SET analysis_artifact_id = (
    SELECT candidate.id
    FROM public.scanner_entity_artifacts candidate
    WHERE candidate.entity_id = entity.id
      AND candidate.stage = 'analysis_result'
    ORDER BY candidate.id DESC
    LIMIT 1
);

ALTER TABLE public.scanner_entities
    ADD CONSTRAINT scanner_entities_analysis_artifact_id_fkey
        FOREIGN KEY (analysis_artifact_id)
        REFERENCES public.scanner_entity_artifacts(id)
        ON DELETE SET NULL;

CREATE INDEX scanner_entities_scope_generation_idx
    ON public.scanner_entities (library_id, media_type, scope_key, pipeline_generation);

CREATE INDEX scanner_entity_artifacts_generation_idx
    ON public.scanner_entity_artifacts (entity_id, pipeline_generation, stage, id DESC);

CREATE INDEX scanner_entity_artifacts_source_idx
    ON public.scanner_entity_artifacts (source_artifact_id)
    WHERE source_artifact_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS public.scanner_entity_artifacts_source_idx;
DROP INDEX IF EXISTS public.scanner_entity_artifacts_generation_idx;
DROP INDEX IF EXISTS public.scanner_entities_scope_generation_idx;

ALTER TABLE public.scanner_entities
    DROP CONSTRAINT IF EXISTS scanner_entities_analysis_artifact_id_fkey;

ALTER TABLE public.scanner_entity_artifacts
    DROP CONSTRAINT IF EXISTS scanner_entity_artifacts_source_artifact_id_fkey,
    DROP CONSTRAINT IF EXISTS scanner_entity_artifacts_pipeline_generation_positive,
    DROP COLUMN IF EXISTS source_artifact_id,
    DROP COLUMN IF EXISTS pipeline_generation;

ALTER TABLE public.scanner_entities
    DROP CONSTRAINT IF EXISTS scanner_entities_pipeline_generation_positive,
    DROP COLUMN IF EXISTS pipeline_generation,
    DROP COLUMN IF EXISTS analysis_artifact_id;
