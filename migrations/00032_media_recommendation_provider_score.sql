-- +goose Up

-- HeyaMetadata's provider_score is a provider-native recommendation ranking
-- (for example TMDB popularity), not a 0-10 vote average. Keep it separate so
-- values above 99.9 neither overflow the legacy NUMERIC(3,1) rating column nor
-- render as impossible star ratings.
ALTER TABLE public.media_recommendations
    ADD COLUMN provider_score double precision NOT NULL DEFAULT 0;

-- Availability is supplied in the already-batched release projection. Persist
-- it on the local track so list/detail responses can show a badge without
-- probing the full recording lyrics endpoint per row.
ALTER TABLE public.tracks
    ADD COLUMN lyrics_available boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE public.tracks DROP COLUMN lyrics_available;
ALTER TABLE public.media_recommendations DROP COLUMN provider_score;
