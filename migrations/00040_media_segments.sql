-- +goose Up

-- Skip segments (intro / recap / credits / preview / commercial markers)
-- per playable file. Rows are the PICKED winners after the duration gate —
-- heya.media returns every community candidate with provenance, the
-- segments worker chooses per type using the file's actual runtime, and
-- only accepted markers land here. end_ms is always materialized (open
-- "to end of media" markers get the file duration) so consumers never
-- handle nulls.
--
-- source records where the winner came from (community:theintrodb,
-- community:skipmedb, community:aniskip; later: chapter, chromaprint,
-- blackframe, manual) — precedence on refresh and the future
-- contribute-back filter both key off it.
CREATE TABLE media_segments (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    library_file_id BIGINT      NOT NULL REFERENCES library_files(id) ON DELETE CASCADE,
    segment_type    TEXT        NOT NULL, -- intro | recap | credits | preview | commercial
    start_ms        BIGINT      NOT NULL,
    end_ms          BIGINT      NOT NULL,
    source          TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_media_segments_file ON media_segments (library_file_id);

-- Pending sentinel for the segments pump (mirrors fingerprinted_at /
-- loudness_analyzed_at). NULL = never checked. A non-NULL timestamp with
-- zero media_segments rows means "checked, community had nothing" — the
-- pump re-checks those after 7 days since the community DBs grow.
ALTER TABLE library_files ADD COLUMN segments_analyzed_at TIMESTAMPTZ;

INSERT INTO scheduled_tasks (id, display_name, description, category, enabled)
VALUES (
    'scan_media_segments',
    'Fetch Skip Segments',
    'Community intro/credits skip markers from heya.media for movie and episode files',
    'library',
    true
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM scheduled_tasks WHERE id = 'scan_media_segments';
ALTER TABLE library_files DROP COLUMN segments_analyzed_at;
DROP TABLE media_segments;
