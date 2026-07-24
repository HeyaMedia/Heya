-- +goose Up

-- Provider-backed radio ranks a local track by either exact recording MBID
-- or case-insensitive title within one artist. The old correlated predicate
-- combined both paths with OR, forcing a scan of every provider track for
-- every local candidate (13s in production). Keep the two lookup paths
-- independently indexable; rank/provider_rank are included so MIN can stay
-- index-only.
CREATE INDEX idx_artist_top_tracks_artist_mbid_rank
    ON public.artist_top_tracks (artist_id, mbid)
    INCLUDE (rank, provider_rank)
    WHERE mbid <> '';

CREATE INDEX idx_artist_top_tracks_artist_title_rank
    ON public.artist_top_tracks (artist_id, lower(title))
    INCLUDE (rank, provider_rank);

-- Recommendation rows repeatedly display global completed-play counts.
-- This partial index turns each per-candidate count into a tiny index-only
-- probe instead of filtering heap rows from the general track index.
CREATE INDEX idx_play_events_completed_track
    ON public.play_events (track_id)
    WHERE completed;

-- +goose Down

DROP INDEX IF EXISTS public.idx_play_events_completed_track;
DROP INDEX IF EXISTS public.idx_artist_top_tracks_artist_title_rank;
DROP INDEX IF EXISTS public.idx_artist_top_tracks_artist_mbid_rank;
