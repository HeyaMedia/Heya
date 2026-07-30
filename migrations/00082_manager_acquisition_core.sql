-- +goose Up
-- Shadow acquisition pipeline, core schema (slice 3 of the dry-run arr
-- replacement). Everything the engine decides is persisted with enough
-- snapshot data that history stays readable after profiles change, items
-- get deleted, or indexers disappear — the ledger is the product.

-- Immutable policy snapshots, referenced by hash. One row can back thousands
-- of decisions; the snapshot carries the FULL evaluation policy (profile
-- ladder/thresholds/language, custom-format definitions, size-definition
-- table + version, comparer/evaluator/parser versions).
CREATE TABLE manager_policy_snapshots (
    policy_hash text PRIMARY KEY,
    snapshot    jsonb NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- One run = one operation: an RSS sweep, a search, an interactive search,
-- an add, an add-triggered search, a reevaluation pass, a queue-verdict
-- pass. Execution status lives here (decisions carry only verdicts).
CREATE TABLE manager_runs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text NOT NULL CHECK (kind IN ('rss','search','interactive','add','add_search','reevaluate','queue_verdict')),
    source      text NOT NULL CHECK (source IN ('scheduled','api','cli')),
    status      text NOT NULL DEFAULT 'running' CHECK (status IN ('running','completed','failed')),
    partial     boolean NOT NULL DEFAULT false,
    truncated   boolean NOT NULL DEFAULT false,
    scope       jsonb NOT NULL DEFAULT '{}'::jsonb,
    stats       jsonb NOT NULL DEFAULT '{}'::jsonb,
    errors      jsonb NOT NULL DEFAULT '[]'::jsonb,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX idx_manager_runs_started ON manager_runs (started_at DESC, id DESC);
CREATE INDEX idx_manager_runs_running ON manager_runs (id) WHERE status = 'running';

-- Per-(indexer, domain) aggregate within a run: "queried fine with zero
-- results" must stay distinguishable from "timed out" and "skipped".
CREATE TABLE manager_run_indexers (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id        bigint NOT NULL REFERENCES manager_runs(id) ON DELETE CASCADE,
    indexer_id    bigint REFERENCES manager_indexers(id) ON DELETE SET NULL,
    indexer_name  text NOT NULL,
    domain        text NOT NULL CHECK (domain IN ('movie','tv','music','book')),
    status        text NOT NULL CHECK (status IN ('ok','failed','skipped_disabled','skipped_backoff','skipped_unsupported_protocol','truncated')),
    pages_fetched integer NOT NULL DEFAULT 0 CHECK (pages_fetched >= 0),
    fetched       integer NOT NULL DEFAULT 0 CHECK (fetched >= 0),
    duration_ms   bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    error         text NOT NULL DEFAULT ''
);
CREATE INDEX idx_manager_run_indexers_run ON manager_run_indexers (run_id);

-- One row per actual HTTP query (id-first search, fallback search, RSS page
-- N...). Params are secret-swept before persisting.
CREATE TABLE manager_run_requests (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_indexer_id bigint NOT NULL REFERENCES manager_run_indexers(id) ON DELETE CASCADE,
    ordinal        integer NOT NULL CHECK (ordinal >= 0),
    params         jsonb NOT NULL DEFAULT '{}'::jsonb,
    page_offset    integer NOT NULL DEFAULT 0 CHECK (page_offset >= 0),
    results        integer NOT NULL DEFAULT 0 CHECK (results >= 0),
    duration_ms    bigint NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    error          text NOT NULL DEFAULT '',
    UNIQUE (run_indexer_id, ordinal)
);

-- Durable release identity: one row per distinct release per (indexer,
-- domain). RSS and search results both land here — the anti-survivor-bias
-- ledger records what we SAW, not just what we liked. Aggregates only;
-- per-appearance state lives on sightings.
CREATE TABLE manager_releases (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    indexer_id       bigint REFERENCES manager_indexers(id) ON DELETE SET NULL,
    indexer_name     text NOT NULL,
    domain           text NOT NULL CHECK (domain IN ('movie','tv','music','book')),
    release_key      text NOT NULL,
    ui_fingerprint   text NOT NULL DEFAULT '',
    guid             text,
    title            text NOT NULL,
    size_bytes       bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    publish_date     timestamptz,
    publish_date_raw text NOT NULL DEFAULT '',
    categories       integer[] NOT NULL DEFAULT '{}',
    raw_attrs        jsonb NOT NULL DEFAULT '[]'::jsonb,
    info_url         text NOT NULL DEFAULT '',
    first_seen_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at     timestamptz NOT NULL DEFAULT now(),
    times_seen       integer NOT NULL DEFAULT 1 CHECK (times_seen >= 1)
);
CREATE UNIQUE INDEX idx_manager_releases_identity
    ON manager_releases (indexer_id, domain, release_key);
CREATE INDEX idx_manager_releases_last_seen ON manager_releases (last_seen_at);
CREATE INDEX idx_manager_releases_fingerprint ON manager_releases (ui_fingerprint) WHERE ui_fingerprint <> '';

-- One row per (release, run request): the retained evaluation work item.
-- Records which request returned it, its response-specific attributes when
-- they differ, and why THAT run classified it the way it did. Crash
-- recovery re-picks status='pending' rows with attempts < 3.
CREATE TABLE manager_release_sightings (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    release_id     bigint NOT NULL REFERENCES manager_releases(id) ON DELETE CASCADE,
    run_id         bigint REFERENCES manager_runs(id) ON DELETE SET NULL,
    run_request_id bigint REFERENCES manager_run_requests(id) ON DELETE SET NULL,
    seen_at        timestamptz NOT NULL DEFAULT now(),
    response_attrs jsonb,
    status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','evaluated','unmatched','ambiguous','unmonitored','unwanted','failed')),
    attempts       integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error          text NOT NULL DEFAULT '',
    matched        jsonb,
    decision_id    bigint,
    policy_hash    text REFERENCES manager_policy_snapshots(policy_hash)
);
CREATE INDEX idx_manager_release_sightings_status ON manager_release_sightings (status, seen_at);
CREATE INDEX idx_manager_release_sightings_release ON manager_release_sightings (release_id, seen_at DESC);
CREATE INDEX idx_manager_release_sightings_run ON manager_release_sightings (run_id);

-- One candidate = one release evaluated within one run. Snapshots stay
-- inline so the row outlives release pruning and indexer deletion. Never
-- merged across indexers: priority evidence is part of the record.
CREATE TABLE manager_candidates (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id           bigint NOT NULL REFERENCES manager_runs(id) ON DELETE CASCADE,
    release_id       bigint REFERENCES manager_releases(id) ON DELETE SET NULL,
    indexer_id       bigint REFERENCES manager_indexers(id) ON DELETE SET NULL,
    indexer_name     text NOT NULL,
    indexer_priority integer NOT NULL DEFAULT 25,
    title            text NOT NULL,
    size_bytes       bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    publish_date     timestamptz,
    categories       integer[] NOT NULL DEFAULT '{}',
    parsed           jsonb NOT NULL DEFAULT '{}'::jsonb,
    quality          text,
    quality_position integer,
    format_score     integer NOT NULL DEFAULT 0,
    format_breakdown jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- Run-scope rejections only (parse/identity); per-target verdicts live
    -- on manager_candidate_targets.
    rejections       jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    -- Composite-FK anchor: candidate_targets and decision selections prove
    -- same-run membership through (id, run_id).
    UNIQUE (id, run_id),
    UNIQUE (run_id, release_id)
);
CREATE INDEX idx_manager_candidates_run ON manager_candidates (run_id, id);

-- One decision = one atomic target-unit verdict within a run. FKs are all
-- SET NULL + NOT-NULL snapshots: deleting a movie must never delete or
-- blind its acquisition history. music_target_id gets its FK in the music
-- slice migration (manager_music_targets doesn't exist yet).
CREATE TABLE manager_decisions (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id           bigint NOT NULL REFERENCES manager_runs(id) ON DELETE CASCADE,
    decided_at       timestamptz NOT NULL DEFAULT now(),
    target_kind      text NOT NULL CHECK (target_kind IN ('movie','episode','season','music_release','book')),
    target_key       text NOT NULL,
    media_item_id    bigint REFERENCES media_items(id) ON DELETE SET NULL,
    tv_episode_id    bigint REFERENCES tv_episodes(id) ON DELETE SET NULL,
    music_target_id  bigint,
    library_id       bigint NOT NULL,
    domain           text NOT NULL CHECK (domain IN ('movie','tv','music','book')),
    target_title     text NOT NULL,
    target_year      integer NOT NULL DEFAULT 0,
    season_number    integer,
    episode_number   integer,
    absolute_number  integer,
    artist_name      text NOT NULL DEFAULT '',
    album_type       text NOT NULL DEFAULT '',
    edition_key      text NOT NULL DEFAULT '',
    album_title      text NOT NULL DEFAULT '',
    profile_id       bigint REFERENCES manager_quality_profiles(id) ON DELETE SET NULL,
    profile_name     text NOT NULL DEFAULT '',
    policy_hash      text REFERENCES manager_policy_snapshots(policy_hash),
    evaluator_version integer NOT NULL DEFAULT 1,
    parser_version    integer NOT NULL DEFAULT 1,
    verdict          text NOT NULL CHECK (verdict IN ('would_grab','already_satisfied','no_acceptable_candidate','comparison_uncertain','configuration_error')),
    chosen_target_row bigint,
    context          jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Target shape rides on snapshot fields, never live FKs (SET NULL must
    -- not break the CHECK).
    CONSTRAINT manager_decisions_target_shape CHECK (
        CASE target_kind
            WHEN 'episode' THEN season_number IS NOT NULL AND episode_number IS NOT NULL
            WHEN 'season' THEN season_number IS NOT NULL
            WHEN 'music_release' THEN artist_name <> '' AND album_type <> ''
            ELSE true
        END
    ),
    CONSTRAINT manager_decisions_chosen_iff_grab CHECK (
        (verdict = 'would_grab') = (chosen_target_row IS NOT NULL)
    ),
    -- Composite anchor for candidate_targets' same-run proof.
    UNIQUE (id, run_id),
    -- Retries can't mint duplicate atomic decisions inside one run.
    UNIQUE (run_id, target_key)
);
CREATE INDEX idx_manager_decisions_decided ON manager_decisions (decided_at DESC, id DESC);
CREATE INDEX idx_manager_decisions_item ON manager_decisions (media_item_id, decided_at DESC) WHERE media_item_id IS NOT NULL;
CREATE INDEX idx_manager_decisions_episode ON manager_decisions (tv_episode_id, decided_at DESC) WHERE tv_episode_id IS NOT NULL;
CREATE INDEX idx_manager_decisions_music ON manager_decisions (music_target_id, decided_at DESC) WHERE music_target_id IS NOT NULL;
CREATE INDEX idx_manager_decisions_verdict ON manager_decisions (verdict, decided_at DESC);
CREATE INDEX idx_manager_decisions_library ON manager_decisions (library_id, decided_at DESC, id DESC);

-- The per-candidate-per-target verdict matrix. For single-target runs the
-- rows are 1:1 with candidates; for season packs this is what records
-- "pack rejected because it downgrades E03". Both parents are proven to
-- share the run via composite FKs.
CREATE TABLE manager_candidate_targets (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    candidate_id   bigint NOT NULL,
    decision_id    bigint NOT NULL,
    run_id         bigint NOT NULL,
    verdict        text NOT NULL CHECK (verdict IN ('acceptable','rejected')),
    rejections     jsonb NOT NULL DEFAULT '[]'::jsonb,
    selection_rank integer CHECK (selection_rank IS NULL OR selection_rank >= 1),
    FOREIGN KEY (candidate_id, run_id) REFERENCES manager_candidates (id, run_id) ON DELETE CASCADE,
    FOREIGN KEY (decision_id, run_id) REFERENCES manager_decisions (id, run_id) ON DELETE CASCADE,
    UNIQUE (decision_id, candidate_id),
    -- Anchor for the decision's chosen-row FK: selection can only point at
    -- a row that belongs to that same decision.
    UNIQUE (id, decision_id)
);
CREATE INDEX idx_manager_candidate_targets_decision ON manager_candidate_targets (decision_id, selection_rank, candidate_id);
CREATE INDEX idx_manager_candidate_targets_candidate ON manager_candidate_targets (candidate_id);

-- Deferred so decision + targets + selection can commit in one transaction
-- regardless of insert order.
ALTER TABLE manager_decisions
    ADD CONSTRAINT manager_decisions_chosen_fk
    FOREIGN KEY (chosen_target_row, id)
    REFERENCES manager_candidate_targets (id, decision_id)
    DEFERRABLE INITIALLY DEFERRED;

-- Sightings link decisions loosely (SET NULL semantics enforced in app
-- code because the FK would be circular-ish with runs); add the real FK
-- now that decisions exist.
ALTER TABLE manager_release_sightings
    ADD CONSTRAINT manager_release_sightings_decision_fk
    FOREIGN KEY (decision_id) REFERENCES manager_decisions (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE manager_release_sightings DROP CONSTRAINT manager_release_sightings_decision_fk;
ALTER TABLE manager_decisions DROP CONSTRAINT manager_decisions_chosen_fk;
DROP TABLE manager_candidate_targets;
DROP TABLE manager_decisions;
DROP TABLE manager_candidates;
DROP TABLE manager_release_sightings;
DROP TABLE manager_releases;
DROP TABLE manager_run_requests;
DROP TABLE manager_run_indexers;
DROP TABLE manager_runs;
DROP TABLE manager_policy_snapshots;
