-- +goose Up

-- Similar-artist scores are provider-native. Last.fm emits a 0..1 ratio,
-- while Deezer emits an artist's fan count (often hundreds of thousands).
-- The original numeric(6,4) only allowed values below 100 and caused an
-- otherwise successful artist refresh to lose its similar-artist projection.
ALTER TABLE public.artist_similar_artists
    ALTER COLUMN match_score TYPE numeric(23,4);

-- +goose Down

-- Make rollback deterministic even after wider provider scores have landed.
UPDATE public.artist_similar_artists
SET match_score = LEAST(99.9999, GREATEST(-99.9999, match_score));

ALTER TABLE public.artist_similar_artists
    ALTER COLUMN match_score TYPE numeric(6,4);
