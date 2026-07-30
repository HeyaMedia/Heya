-- +goose Up
-- Persisted shadow verdicts for download-client queue items: the live arrs
-- grab releases into SAB; Heya records what IT would have done with each —
-- the release-for-release comparison surface. Client FK is SET NULL +
-- snapshots: deleting a client config must not delete evidence.
CREATE TABLE manager_queue_verdicts (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    download_client_id bigint REFERENCES manager_download_clients(id) ON DELETE SET NULL,
    client_name        text NOT NULL,
    nzo_id             text NOT NULL,
    release_title      text NOT NULL,
    category           text NOT NULL DEFAULT '',
    sab_status_latest  text NOT NULL DEFAULT '',
    first_seen_at      timestamptz NOT NULL DEFAULT now(),
    last_seen_at       timestamptz NOT NULL DEFAULT now(),
    parsed             jsonb NOT NULL DEFAULT '{}'::jsonb,
    matched_media_item_id bigint REFERENCES media_items(id) ON DELETE SET NULL,
    matched_title      text NOT NULL DEFAULT '',
    verdict            text NOT NULL CHECK (verdict IN ('would_accept','would_reject','unknown_identity','unmonitored','no_profile')),
    rejections         jsonb NOT NULL DEFAULT '[]'::jsonb,
    policy_hash        text REFERENCES manager_policy_snapshots(policy_hash),
    evaluation_input_hash text NOT NULL DEFAULT '',
    UNIQUE (download_client_id, nzo_id)
);
CREATE INDEX idx_manager_queue_verdicts_seen ON manager_queue_verdicts (last_seen_at DESC);

-- Append-only verdict history: policy or inventory changes re-evaluate live
-- rows; every change is a new history row, normalized (not a jsonb pile).
CREATE TABLE manager_queue_verdict_history (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    verdict_id   bigint NOT NULL REFERENCES manager_queue_verdicts(id) ON DELETE CASCADE,
    verdict      text NOT NULL,
    rejections   jsonb NOT NULL DEFAULT '[]'::jsonb,
    input_hash   text NOT NULL,
    evaluated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_manager_queue_verdict_history_verdict ON manager_queue_verdict_history (verdict_id, evaluated_at DESC);

-- +goose Down
DROP TABLE manager_queue_verdict_history;
DROP TABLE manager_queue_verdicts;
