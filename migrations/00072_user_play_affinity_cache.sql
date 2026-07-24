-- +goose Up
-- Mixes for You recomputed the recency-decayed play-affinity CTE
-- (LEAST(2.0, SUM(0.25 * POWER(0.5, age/30d)))) from scratch in ~11 separate
-- queries per cold load — ~760ms each on the heaviest user (66k completed
-- plays), i.e. most of the multi-second render. The affinity decays slowly, so
-- materialize it per user and refresh lazily (see ensureUserPlayAffinity); the
-- shared musicAffinityCTE now reads play_aff from here instead of scanning
-- play_events on every call.
CREATE TABLE IF NOT EXISTS user_play_affinity (
    user_id  bigint           NOT NULL,
    track_id bigint           NOT NULL,
    score    double precision NOT NULL,
    PRIMARY KEY (user_id, track_id)
);

CREATE TABLE IF NOT EXISTS user_play_affinity_state (
    user_id      bigint      PRIMARY KEY,
    refreshed_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS user_play_affinity_state;
DROP TABLE IF EXISTS user_play_affinity;
