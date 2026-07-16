-- +goose Up
-- Artist metadata expansion (heya.media 2026-07-16 provider upgrade:
-- audiodb / tidal / bandcamp).
--
-- genres: upstream now distinguishes curated genres from folksonomy tags;
-- previously both were merged into artists.tags. Existing rows keep the
-- merged list in tags until their next refresh separates them.
ALTER TABLE artists ADD COLUMN genres text[] NOT NULL DEFAULT '{}';

-- provider: similar-artist suggestions now arrive from three providers
-- (lastfm / deezer / tidal) whose scores are not comparable — attribution
-- must survive into the row. Pre-fix rows are all Last.fm.
ALTER TABLE artist_similar_artists ADD COLUMN provider text NOT NULL DEFAULT 'lastfm';

-- +goose Down
ALTER TABLE artist_similar_artists DROP COLUMN provider;
ALTER TABLE artists DROP COLUMN genres;
