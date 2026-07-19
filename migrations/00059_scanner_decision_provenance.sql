-- +goose Up

ALTER TABLE public.local_media_identities
    ADD COLUMN decision_provenance text NOT NULL DEFAULT 'legacy',
    ADD COLUMN decision_matcher_revision integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT local_media_identities_decision_provenance_check
        CHECK (decision_provenance IN ('legacy', 'automatic', 'manual')),
    ADD CONSTRAINT local_media_identities_decision_matcher_revision_check
        CHECK (decision_matcher_revision >= 0);

-- Rejections and ignores only existed as explicit review actions. For accepted
-- identities, preserve only decisions with evidence that predates explicit
-- provenance: a rank-zero manual-search candidate, a candidate selected after
-- its scan finished, or any resolved finding whose timestamp was not emitted
-- by a matching scan boundary. The latter is how pre-provenance single/bulk
-- approval resolved review findings, including materialization_blocked. Other
-- accepted rows deliberately remain legacy and re-enter matching once after
-- this upgrade.
WITH scan_boundaries AS MATERIALIZED (
    SELECT library_id, media_type, created_at AS boundary_at
    FROM public.scan_runs
    UNION
    SELECT library_id, media_type, finished_at
    FROM public.scan_runs
    WHERE finished_at IS NOT NULL
),
manual_resolutions AS MATERIALIZED (
    SELECT DISTINCT finding.identity_id
    FROM public.scan_findings finding
    JOIN public.local_media_identities identity ON identity.id = finding.identity_id
    LEFT JOIN scan_boundaries boundary
      ON boundary.library_id = identity.library_id
     AND boundary.media_type = identity.media_type
     AND boundary.boundary_at = finding.resolved_at
    WHERE identity.review_status = 'accepted'
      AND finding.resolved_at IS NOT NULL
      AND boundary.boundary_at IS NULL
),
manual_accepts AS MATERIALIZED (
    SELECT DISTINCT candidate.identity_id
    FROM public.metadata_match_candidates candidate
    JOIN public.local_media_identities identity
      ON identity.id = candidate.identity_id
     AND identity.review_status = 'accepted'
    LEFT JOIN public.scan_runs candidate_run ON candidate_run.id = candidate.scan_run_id
    LEFT JOIN manual_resolutions resolution ON resolution.identity_id = identity.id
    WHERE candidate.status = 'selected'
      AND (
          candidate.rank = 0
          OR (
              candidate_run.finished_at IS NOT NULL
              AND candidate.updated_at > candidate_run.finished_at
          )
          OR resolution.identity_id IS NOT NULL
      )
)
UPDATE public.local_media_identities identity
SET decision_provenance = 'manual'
WHERE identity.review_status IN ('rejected', 'ignored')
   OR identity.id IN (SELECT identity_id FROM manual_accepts);

CREATE INDEX local_media_identities_decision_reuse_idx
    ON public.local_media_identities (
        library_id, media_type, decision_provenance,
        decision_matcher_revision, identity_key
    )
    WHERE review_status IN ('accepted', 'rejected', 'ignored');

-- +goose Down

DROP INDEX IF EXISTS public.local_media_identities_decision_reuse_idx;

ALTER TABLE public.local_media_identities
    DROP CONSTRAINT IF EXISTS local_media_identities_decision_matcher_revision_check,
    DROP CONSTRAINT IF EXISTS local_media_identities_decision_provenance_check,
    DROP COLUMN IF EXISTS decision_matcher_revision,
    DROP COLUMN IF EXISTS decision_provenance;
