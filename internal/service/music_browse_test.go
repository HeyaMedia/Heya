package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// browseGenreFixture seeds one playable album whose artist carries a
// metadata tag and one whose album carries a metadata genre, so both
// branches of the metadata-genre album resolution get exercised.
type browseGenreFixture struct {
	artistTagTrack  int64 // track under the artist tagged with the genre
	albumGenreTrack int64 // track under the album tagged with the genre
}

func setupBrowseGenreFixture(t *testing.T, pool *pgxpool.Pool, userID int64, tag string) browseGenreFixture {
	t.Helper()
	ctx := context.Background()
	var f browseGenreFixture

	var libraryID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO libraries (name, media_type, created_by) VALUES ($1, 'music', $2) RETURNING id`,
		"browse-genre-test", userID).Scan(&libraryID))
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, libraryID) })

	var itemIDs []int64
	addTrack := func(kind string, artistTags, albumGenres string) int64 {
		var itemID, artistID, albumID, trackID, fileID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO media_items (library_id, media_type, slug) VALUES ($1, 'music', $2) RETURNING id`,
			libraryID, "browse-genre-"+kind).Scan(&itemID))
		itemIDs = append(itemIDs, itemID)
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO artists (media_item_id, name, tags) VALUES ($1, $2, $3::text[]) RETURNING id`,
			itemID, "Browse Artist "+kind, artistTags).Scan(&artistID))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO albums (artist_id, title, slug, genres) VALUES ($1, $2, $3, $4::text[]) RETURNING id`,
			artistID, "Browse Album "+kind, "browse-album-"+kind, albumGenres).Scan(&albumID))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO tracks (album_id, disc_number, track_number, title, duration)
			 VALUES ($1, 1, 1, $2, 180) RETURNING id`,
			albumID, "Browse Track "+kind).Scan(&trackID))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO library_files (library_id, path, media_item_id)
			 VALUES ($1, $2, $3) RETURNING id`,
			libraryID, fmt.Sprintf("/music/browse-genre/%s.flac", kind), itemID).Scan(&fileID))
		_, err := pool.Exec(ctx,
			`INSERT INTO track_files (track_id, library_file_id) VALUES ($1, $2)`, trackID, fileID)
		require.NoError(t, err)
		return trackID
	}

	// Mixed case on purpose: the drilldown must match case-insensitively.
	f.artistTagTrack = addTrack("artist-tag", fmt.Sprintf(`{"%s"}`, tag), `{}`)
	f.albumGenreTrack = addTrack("album-genre", `{}`, fmt.Sprintf(`{"%s"}`, tag))

	t.Cleanup(func() {
		for _, id := range itemIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM media_items WHERE id = $1`, id)
		}
	})
	return f
}

func TestBrowseGenreMetadataFallback(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	ctx := context.Background()

	// Not a Discogs-400 classifier label → resolved via artist/album tags.
	const storedTag = "Browse-Test Metalcore Zz"
	const queriedAs = "browse-test metalcore zz"
	f := setupBrowseGenreFixture(t, pool, userID, storedTag)

	total, err := app.CountTracksForGenre(ctx, queriedAs)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)

	rows, err := app.ListTracksByGenre(ctx, queriedAs, 50, 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	got := []int64{rows[0].TrackID, rows[1].TrackID}
	require.ElementsMatch(t, []int64{f.artistTagTrack, f.albumGenreTrack}, got)

	// A name nobody carries resolves to zero albums and an empty page.
	total, err = app.CountTracksForGenre(ctx, "browse-test no-such-genre")
	require.NoError(t, err)
	require.Zero(t, total)
	rows, err = app.ListTracksByGenre(ctx, "browse-test no-such-genre", 50, 0)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestBrowseGenreSonicPathStillServed(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	ctx := context.Background()

	f := setupBrowseGenreFixture(t, pool, userID, "Browse-Test Sonic Zz")
	_, err := pool.Exec(ctx,
		`INSERT INTO track_facets (track_id, top_genres, analyzer_version)
		 VALUES ($1, '[{"name": "Rock---Metalcore", "score": 0.91}]'::jsonb, 1)`,
		f.artistTagTrack)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM track_facets WHERE track_id = $1`, f.artistTagTrack)
	})

	// Lowercase input canonicalizes to the classifier label and takes the
	// sonic bucket, not the metadata tags (which don't carry this name).
	rows, err := app.ListTracksByGenre(ctx, "rock---metalcore", 50, 0)
	require.NoError(t, err)
	var ids []int64
	for _, r := range rows {
		ids = append(ids, r.TrackID)
	}
	require.Contains(t, ids, f.artistTagTrack)
	require.NotContains(t, ids, f.albumGenreTrack)
}

// browseFacetFixture seeds one artist with 3 distinct-recording tracks, all
// carrying the same mood/genre/tempo facets — 3 tracks clears
// genreMinTrackHits (the ListGenreBuckets HAVING floor) so the genre bucket
// actually surfaces, and lets the same fixture exercise all three bucket
// kinds (moods, genres, tempo) plus their per-bucket top-artist rankings in
// one pass.
type browseFacetFixture struct {
	artistMediaItemID       int64
	artistMediaItemPublicID string
}

func setupBrowseFacetFixture(t *testing.T, pool *pgxpool.Pool, userID int64) browseFacetFixture {
	t.Helper()
	ctx := context.Background()
	var f browseFacetFixture

	var libraryID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO libraries (name, media_type, created_by) VALUES ($1, 'music', $2) RETURNING id`,
		"browse-facet-test", userID).Scan(&libraryID))
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, libraryID) })

	var itemID, artistID, albumID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO media_items (library_id, media_type, slug) VALUES ($1, 'music', 'browse-facet-artist') RETURNING id`,
		libraryID).Scan(&itemID))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT public_id::text FROM media_items WHERE id = $1`, itemID).Scan(&f.artistMediaItemPublicID))
	f.artistMediaItemID = itemID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO artists (media_item_id, name) VALUES ($1, $2) RETURNING id`,
		itemID, "Browse Facet Artist").Scan(&artistID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO albums (artist_id, title, slug) VALUES ($1, 'Browse Facet Album', 'browse-facet-album') RETURNING id`,
		artistID).Scan(&albumID))

	const topGenres = `[{"name": "Electronic---Techno", "score": 0.9}]`
	const moodTags = `{"mood_happy": 0.9}`
	for i := 1; i <= 3; i++ {
		var trackID, fileID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO tracks (album_id, disc_number, track_number, title, duration)
			 VALUES ($1, 1, $2, $3, 180) RETURNING id`,
			albumID, i, fmt.Sprintf("Browse Facet Track %d", i)).Scan(&trackID))
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO library_files (library_id, path, media_item_id)
			 VALUES ($1, $2, $3) RETURNING id`,
			libraryID, fmt.Sprintf("/music/browse-facet/%d.flac", i), itemID).Scan(&fileID))
		_, err := pool.Exec(ctx,
			`INSERT INTO track_files (track_id, library_file_id) VALUES ($1, $2)`, trackID, fileID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx,
			`INSERT INTO track_facets (track_id, top_genres, mood_tags, bpm, analyzer_version)
			 VALUES ($1, $2::jsonb, $3::jsonb, 125, 1)`,
			trackID, topGenres, moodTags)
		require.NoError(t, err)
	}

	return f
}

func TestBrowseBucketsCarryTopArtists(t *testing.T) {
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	userID := testutil.TestUserID(t, pool)
	ctx := context.Background()

	f := setupBrowseFacetFixture(t, pool, userID)
	wantArtist := BrowseBucketArtist{ID: f.artistMediaItemID, PublicID: f.artistMediaItemPublicID}

	moods, err := app.ListMoodBuckets(ctx)
	require.NoError(t, err)
	happy, ok := findMoodBucket(moods, "mood_happy")
	require.True(t, ok, "mood_happy bucket missing")
	require.GreaterOrEqual(t, happy.TrackCount, int64(3))
	require.Contains(t, happy.Artists, wantArtist)
	// Every bucket keeps its Artists field present (possibly empty), never nil.
	for _, b := range moods {
		require.NotNil(t, b.Artists)
	}

	tempo, err := app.ListTempoBuckets(ctx)
	require.NoError(t, err)
	band, ok := findTempoBucket(tempo, "110-130")
	require.True(t, ok, "110-130 tempo bucket missing")
	require.GreaterOrEqual(t, band.TrackCount, int64(3))
	require.Contains(t, band.Artists, wantArtist)
	for _, b := range tempo {
		require.NotNil(t, b.Artists)
	}

	genres, err := app.ListGenreBuckets(ctx)
	require.NoError(t, err)
	genre, ok := findGenreBucket(genres, "Electronic---Techno")
	require.True(t, ok, "Electronic---Techno genre bucket missing")
	require.GreaterOrEqual(t, genre.TrackCount, int64(3))
	require.Contains(t, genre.Artists, wantArtist)
	for _, b := range genres {
		require.NotNil(t, b.Artists)
	}
}

func findMoodBucket(buckets []MoodBucket, key string) (MoodBucket, bool) {
	for _, b := range buckets {
		if b.Key == key {
			return b, true
		}
	}
	return MoodBucket{}, false
}

func findTempoBucket(buckets []TempoBucket, key string) (TempoBucket, bool) {
	for _, b := range buckets {
		if b.Key == key {
			return b, true
		}
	}
	return TempoBucket{}, false
}

func findGenreBucket(buckets []GenreBucket, name string) (GenreBucket, bool) {
	for _, b := range buckets {
		if b.Name == name {
			return b, true
		}
	}
	return GenreBucket{}, false
}
