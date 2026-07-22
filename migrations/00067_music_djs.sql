-- +goose Up

-- DJ state belongs to the server-owned, per-device play queue. A monotonically
-- increasing session makes an in-flight recommendation harmless after the DJ
-- is switched, disabled, or the queue is replaced.
ALTER TABLE public.play_queues
    ADD COLUMN dj_mode text NOT NULL DEFAULT 'off',
    ADD COLUMN dj_session bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT play_queues_dj_mode_check
        CHECK (dj_mode IN ('off', 'echo', 'flow', 'voyage', 'encore', 'spotlight', 'timewarp'));

-- Generated ownership stays on each queue item. This lets Heya remove only
-- future tracks contributed by a DJ while preserving the user's queue and
-- already-played history. processed_session prevents revisiting an earlier
-- anchor from generating the same insertion twice within one DJ session.
ALTER TABLE public.play_queue_items
    ADD COLUMN dj_session bigint NOT NULL DEFAULT 0,
    ADD COLUMN dj_mode text NOT NULL DEFAULT '',
    ADD COLUMN dj_processed_session bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT play_queue_items_dj_mode_check
        CHECK (dj_mode IN ('', 'echo', 'flow', 'voyage', 'encore', 'spotlight', 'timewarp'));

CREATE INDEX play_queue_items_dj_session_idx
    ON public.play_queue_items (queue_id, dj_session, ord)
    WHERE dj_session > 0;

-- +goose Down

DROP INDEX IF EXISTS public.play_queue_items_dj_session_idx;

ALTER TABLE public.play_queue_items
    DROP CONSTRAINT play_queue_items_dj_mode_check,
    DROP COLUMN dj_processed_session,
    DROP COLUMN dj_mode,
    DROP COLUMN dj_session;

ALTER TABLE public.play_queues
    DROP CONSTRAINT play_queues_dj_mode_check,
    DROP COLUMN dj_session,
    DROP COLUMN dj_mode;
