-- +goose Up

-- Trailing-edge debounce table for child-content enrichment. When the
-- matcher creates a new child row (album / track / season / episode)
-- under an already-complete media_item, it upserts a row here with
-- `fire_at = now() + delay`. The DebounceSweep periodic worker pulls
-- due rows, kicks off a forced enrich, and deletes the row — all in
-- one transaction so a failed enrich keeps the row alive for the next
-- sweep tick.
--
-- Cardinality is naturally bounded by count(media_items) thanks to the
-- single-column primary key + ON DELETE CASCADE: at most one pending
-- debounce per item, and a media_item deletion wipes its debounce.
-- Repeated upserts within the window collapse into the same row with
-- a pushed-forward fire_at — which is exactly the trailing-edge behavior
-- (e.g. 5000 file matches in 90s → 1 enrich fired 30s after the last).
CREATE TABLE debounced_enriches (
    media_item_id BIGINT      PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
    fire_at       TIMESTAMPTZ NOT NULL,
    -- Free-form tag for the caller. Used today only for log lines and
    -- diagnostic queries (`SELECT requested_by, count(*) FROM
    -- debounced_enriches GROUP BY 1`); kept short to avoid bloating
    -- a hot upsert path.
    requested_by  TEXT        NOT NULL DEFAULT 'matcher'
);

-- The sweep query scans for rows where fire_at <= now(); a btree on
-- fire_at makes that index-only and keeps the worker tick cheap even
-- if the table briefly holds thousands of rows during heavy churn.
CREATE INDEX idx_debounced_enriches_fire_at ON debounced_enriches (fire_at);

-- +goose Down

DROP TABLE debounced_enriches;
