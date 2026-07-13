-- +goose Up

ALTER TABLE public.play_queues
    ADD COLUMN device_id text NOT NULL DEFAULT 'legacy';

ALTER TABLE public.play_queues DROP CONSTRAINT play_queues_user_id_key;
ALTER TABLE public.play_queues
    ADD CONSTRAINT play_queues_user_device_key UNIQUE (user_id, device_id);

-- Existing installations retain their queue as a fallback until the first
-- named client queue is created. New clients always send a stable device id.

-- +goose Down

ALTER TABLE public.play_queues DROP CONSTRAINT play_queues_user_device_key;
ALTER TABLE public.play_queues ADD CONSTRAINT play_queues_user_id_key UNIQUE (user_id);
ALTER TABLE public.play_queues DROP COLUMN device_id;
