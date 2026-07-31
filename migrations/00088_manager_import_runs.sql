-- +goose Up
-- Manual imports are recorded runs: what was imported, where it landed, and
-- what the tagger matched it to.
ALTER TABLE manager_runs DROP CONSTRAINT manager_runs_kind_check;
ALTER TABLE manager_runs ADD CONSTRAINT manager_runs_kind_check
    CHECK (kind IN ('rss','search','interactive','add','add_search','reevaluate','queue_verdict','import'));

-- +goose Down
ALTER TABLE manager_runs DROP CONSTRAINT manager_runs_kind_check;
ALTER TABLE manager_runs ADD CONSTRAINT manager_runs_kind_check
    CHECK (kind IN ('rss','search','interactive','add','add_search','reevaluate','queue_verdict'));
