-- +goose Up

-- Music favorites are now the unified rating store (heart = rating 10): the
-- web app's reactions, Subsonic stars, and Jellyfin favorites all read/write
-- rating bands. Adopt any legacy boolean user_favorites music rows as hearts,
-- then drop them — video/book favorites keep using user_favorites.
INSERT INTO public.user_track_ratings (user_id, track_id, rating)
SELECT uf.user_id, uf.entity_id, 10
FROM public.user_favorites uf
JOIN public.tracks t ON t.id = uf.entity_id
WHERE uf.entity_type = 'track'
ON CONFLICT (user_id, track_id) DO NOTHING;

INSERT INTO public.user_album_ratings (user_id, album_id, rating)
SELECT uf.user_id, uf.entity_id, 10
FROM public.user_favorites uf
JOIN public.albums al ON al.id = uf.entity_id
WHERE uf.entity_type = 'album'
ON CONFLICT (user_id, album_id) DO NOTHING;

INSERT INTO public.user_artist_ratings (user_id, artist_id, rating)
SELECT uf.user_id, uf.entity_id, 10
FROM public.user_favorites uf
JOIN public.artists ar ON ar.id = uf.entity_id
WHERE uf.entity_type = 'artist'
ON CONFLICT (user_id, artist_id) DO NOTHING;

DELETE FROM public.user_favorites WHERE entity_type IN ('track', 'album', 'artist');

-- +goose Down

-- Data adoption — no structural change to revert.
SELECT 1;
