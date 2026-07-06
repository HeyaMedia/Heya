-- +goose Up

-- heya.media now emits the full franchise membership list on a movie's
-- collection block (payload.collection.parts[]): every film in the collection
-- with its tmdb_id/year/poster/vote, including titles not in the local library.
-- Persist it verbatim so the collection detail page can render "you own 3 of
-- 5" and surface the missing entries, without a per-view upstream fetch.
--
-- Shape (array): [{ "title", "year", "tmdb_id", "poster_path", "vote_average" }]
-- Ordered by release (undated entries last), exactly as heya.media returns it.
ALTER TABLE collections ADD COLUMN parts JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE collections DROP COLUMN parts;
