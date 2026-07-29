-- +goose Up
-- Full per-artist discography from the metadata provider — the catalog of
-- release groups that EXIST, not just what's on disk. The albums table stays
-- local-first (rows exist because files exist) so nothing fileless leaks into
-- players, mixes, or the compat APIs; this table is the manager's lens for
-- "what's missing", the way tv_episodes already is for TV. Rows are replaced
-- wholesale per artist on every enrich/refresh (the provider fetch already
-- returns the complete discography), with album_id linking entries the
-- library actually has.
CREATE TABLE artist_discography (
    id bigint GENERATED ALWAYS AS IDENTITY NOT NULL,
    artist_id bigint NOT NULL,
    -- heya.media release-group entity id when resolved; '' for unresolved
    -- relation evidence that still names a real release
    canonical_id text DEFAULT ''::text NOT NULL,
    title text NOT NULL,
    -- 'album' | 'single' | 'ep' | 'compilation' | ... (provider vocabulary)
    album_type text DEFAULT 'album'::text NOT NULL,
    secondary_types text[] DEFAULT '{}'::text[] NOT NULL,
    release_date date,
    year text DEFAULT ''::text NOT NULL,
    track_count integer DEFAULT 0 NOT NULL,
    external_ids jsonb DEFAULT '{}'::jsonb NOT NULL,
    cover_url text DEFAULT ''::text NOT NULL,
    -- matched local album, when the library has this release group
    album_id bigint,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT artist_discography_pkey PRIMARY KEY (id),
    CONSTRAINT artist_discography_artist_id_fkey FOREIGN KEY (artist_id) REFERENCES artists(id) ON DELETE CASCADE,
    CONSTRAINT artist_discography_album_id_fkey FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE SET NULL
);

CREATE INDEX artist_discography_artist_idx ON artist_discography (artist_id);
CREATE INDEX artist_discography_album_idx ON artist_discography (album_id) WHERE album_id IS NOT NULL;

-- +goose Down
DROP TABLE artist_discography;
