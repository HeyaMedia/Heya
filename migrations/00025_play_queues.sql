-- +goose Up

-- Server-owned play queue (docs/queue-plan.md): ONE queue per user, fully
-- materialized, windowed reads. Clients are mirrors — every mutation goes
-- through the service, bumps `version`, and fans out per-user over WS.
CREATE TABLE IF NOT EXISTS public.play_queues (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL UNIQUE,
    -- Concurrency spine: bumped on every STRUCTURAL mutation (items,
    -- pointer, modes, output — not heartbeats). Clients refetch their
    -- window when they see a gap.
    version bigint NOT NULL DEFAULT 0,
    -- Soft pointer into play_queue_items (no FK: a cascaded track delete
    -- may remove the row; the service advances past dangling pointers).
    current_item_id bigint,
    -- Coarse renderer-reported position — the "open the phone 45 minutes
    -- later" answer, not a playback clock.
    position_seconds real NOT NULL DEFAULT 0,
    playing boolean NOT NULL DEFAULT false,
    repeat_mode text NOT NULL DEFAULT 'off',
    shuffled boolean NOT NULL DEFAULT false,
    -- Provenance of the last materialization ({kind, id, genre, shuffle})
    -- for re-shuffle / "more like this" / radio-mode later.
    source jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- 'local:<client_id>' | 'cast:<device_id>' | '' — the one output
    -- allowed to render + advance (Spotify Connect semantics).
    active_output text NOT NULL DEFAULT '',
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT play_queues_repeat_mode_check CHECK (repeat_mode IN ('off', 'all', 'one')),
    CONSTRAINT play_queues_user_id_fkey FOREIGN KEY (user_id)
        REFERENCES public.users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS public.play_queue_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    queue_id bigint NOT NULL,
    -- Sparse ordering key (gap 1024): a reorder is a one-row UPDATE into a
    -- gap; renumbering (rare) rewrites into a fresh range ABOVE max(ord)
    -- so the unique constraint never sees a transient collision.
    ord bigint NOT NULL,
    track_id bigint NOT NULL,
    -- Rank in the source's NATURAL order, captured at materialization —
    -- what "shuffle off" restores without re-querying the source.
    src_ord integer NOT NULL DEFAULT 0,
    CONSTRAINT play_queue_items_queue_ord_key UNIQUE (queue_id, ord),
    CONSTRAINT play_queue_items_queue_id_fkey FOREIGN KEY (queue_id)
        REFERENCES public.play_queues(id) ON DELETE CASCADE,
    CONSTRAINT play_queue_items_track_id_fkey FOREIGN KEY (track_id)
        REFERENCES public.tracks(id) ON DELETE CASCADE
);

-- Cascade cleanup path for track deletions (scanner missing-file sweeps).
CREATE INDEX IF NOT EXISTS play_queue_items_track_idx
    ON public.play_queue_items (track_id);

-- +goose Down

DROP TABLE IF EXISTS public.play_queue_items;
DROP TABLE IF EXISTS public.play_queues;
