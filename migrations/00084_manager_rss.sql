-- +goose Up
-- Durable RSS watermarks: one cursor per (indexer, domain). Never pruned —
-- the cursor is what makes "did we miss a window?" answerable after any
-- crash or truncated poll.
CREATE TABLE manager_rss_cursors (
    indexer_id        bigint NOT NULL REFERENCES manager_indexers(id) ON DELETE CASCADE,
    domain            text NOT NULL CHECK (domain IN ('movie','tv','music','book')),
    last_release_key  text NOT NULL DEFAULT '',
    last_publish_date timestamptz,
    last_run_id       bigint,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (indexer_id, domain)
);

-- +goose Down
DROP TABLE manager_rss_cursors;
