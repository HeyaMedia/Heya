package matcher

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// These exercise the collision-safe artist merge against a real Postgres. Each
// runs entirely inside a transaction that is rolled back, so the database is
// never mutated. They skip in -short mode or when no DB is reachable.

func mergeTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping DB integration test in short mode")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://heya:heya@localhost:5440/heya?sslmode=disable"
	}
	pool, err := database.Connect(context.Background(), url)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	return pool
}

// seedArtist creates a music library media_item + artist with one album and the
// given (disc, track_number, title) tracks. Returns the artist and album IDs.
func seedArtist(t *testing.T, ctx context.Context, qtx *sqlc.Queries, userID, libID int64, name, albumTitle, albumYear string, tracks [][3]any) (artistID, albumID int64) {
	t.Helper()
	item, err := qtx.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:   libID,
		MediaType:   sqlc.MediaTypeMusic,
		Title:       name,
		SortTitle:   name,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := qtx.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: name})
	require.NoError(t, err)
	album, err := qtx.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: albumTitle, Year: albumYear,
		Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)
	for _, tr := range tracks {
		_, err := qtx.CreateTrack(ctx, sqlc.CreateTrackParams{
			AlbumID:     album.ID,
			DiscNumber:  int32(tr[0].(int)),
			TrackNumber: int32(tr[1].(int)),
			Title:       tr[2].(string),
		})
		require.NoError(t, err)
	}
	return artist.ID, album.ID
}

func seedUserAndMusicLib(t *testing.T, ctx context.Context, qtx *sqlc.Queries) (userID, libID int64) {
	t.Helper()
	user, err := qtx.CreateUser(ctx, sqlc.CreateUserParams{
		Username: "mergetest", Email: "mergetest@example.com", PasswordHash: "x", IsAdmin: true,
	})
	require.NoError(t, err)
	lib, err := qtx.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "Music", MediaType: sqlc.MediaTypeMusic, Paths: []string{"/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    user.ID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	return user.ID, lib.ID
}

func TestMergeArtistIntoTx_AlbumCollision(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	userID, libID := seedUserAndMusicLib(t, ctx, qtx)

	// dst and src both carry "Reborn" (2020). dst tracks: (1,1),(1,2).
	// src tracks: (1,1) collides, (1,3) is unique.
	dstArtist, dstAlbum := seedArtist(t, ctx, qtx, userID, libID, "HANABIE", "Reborn", "2020",
		[][3]any{{1, 1, "Song One"}, {1, 2, "Song Two"}})
	srcArtist, srcAlbum := seedArtist(t, ctx, qtx, userID, libID, "Hanabie Kana", "Reborn", "2020",
		[][3]any{{1, 1, "Song One Dup"}, {1, 3, "Song Three"}})

	// Track-scoped user data on the colliding (1,1) pair: a rating on each side
	// (GREATEST must win), plus a playlist entry and a play event on the src
	// side that must survive the merge instead of CASCADE-ing away when the
	// colliding src track is deleted.
	srcHit := trackAt(t, ctx, qtx, srcAlbum, 1, 1)
	dstHit := trackAt(t, ctx, qtx, dstAlbum, 1, 1)
	_, err = tx.Exec(ctx, `INSERT INTO user_track_ratings (user_id, track_id, rating) VALUES ($1,$2,9)`, userID, srcHit)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO user_track_ratings (user_id, track_id, rating) VALUES ($1,$2,5)`, userID, dstHit)
	require.NoError(t, err)
	var playlistID int64
	require.NoError(t, tx.QueryRow(ctx, `INSERT INTO user_playlists (user_id, name) VALUES ($1,'P') RETURNING id`, userID).Scan(&playlistID))
	_, err = tx.Exec(ctx, `INSERT INTO user_playlist_tracks (playlist_id, track_id, position) VALUES ($1,$2,0)`, playlistID, srcHit)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO play_events (user_id, track_id, listened_seconds) VALUES ($1,$2,120)`, userID, srcHit)
	require.NoError(t, err)

	merged, err := mergeArtistIntoTx(ctx, qtx, dstArtist, srcArtist)
	require.NoError(t, err)
	require.True(t, merged)

	// src artist is gone.
	_, err = qtx.GetArtistByID(ctx, srcArtist)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// dst has exactly one album (the duplicate "Reborn" folded in, not added).
	albums, err := qtx.ListAlbumsByArtist(ctx, dstArtist)
	require.NoError(t, err)
	require.Len(t, albums, 1)
	require.Equal(t, dstAlbum, albums[0].ID)

	// dst album holds the union by (disc, track_number): (1,1),(1,2),(1,3).
	tracks, err := qtx.ListTracksByAlbum(ctx, dstAlbum)
	require.NoError(t, err)
	require.Len(t, tracks, 3)
	seen := map[int32]string{}
	for _, tr := range tracks {
		seen[tr.TrackNumber] = tr.Title
	}
	require.Equal(t, "Song One", seen[1]) // dst's track survived the collision
	require.Equal(t, "Song Two", seen[2])
	require.Equal(t, "Song Three", seen[3]) // src's unique track moved over

	// The colliding src track's user data folded onto the dst track rather than
	// vanishing with it.
	var rating int
	require.NoError(t, tx.QueryRow(ctx, `SELECT rating FROM user_track_ratings WHERE user_id=$1 AND track_id=$2`, userID, dstHit).Scan(&rating))
	require.Equal(t, 9, rating) // GREATEST(dst 5, src 9)

	var plCount int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM user_playlist_tracks WHERE playlist_id=$1 AND track_id=$2`, playlistID, dstHit).Scan(&plCount))
	require.Equal(t, 1, plCount) // playlist entry re-pointed at dst track

	var peCount int
	require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM play_events WHERE track_id=$1`, dstHit).Scan(&peCount))
	require.Equal(t, 1, peCount) // play event re-pointed at dst track

	// Nothing dangles on the deleted src track.
	var orphans int
	require.NoError(t, tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM user_track_ratings  WHERE track_id=$1) +
		(SELECT count(*) FROM user_playlist_tracks WHERE track_id=$1) +
		(SELECT count(*) FROM play_events          WHERE track_id=$1)`, srcHit).Scan(&orphans))
	require.Equal(t, 0, orphans)
}

// trackAt returns the id of the track at (disc, num) within an album.
func trackAt(t *testing.T, ctx context.Context, qtx *sqlc.Queries, albumID int64, disc, num int32) int64 {
	t.Helper()
	tracks, err := qtx.ListTracksByAlbum(ctx, albumID)
	require.NoError(t, err)
	for _, tr := range tracks {
		if tr.DiscNumber == disc && tr.TrackNumber == num {
			return tr.ID
		}
	}
	t.Fatalf("no track at disc %d num %d in album %d", disc, num, albumID)
	return 0
}

func TestMergeArtistIntoTx_NoCollision(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	userID, libID := seedUserAndMusicLib(t, ctx, qtx)
	dstArtist, _ := seedArtist(t, ctx, qtx, userID, libID, "Alpha", "Alpha LP", "2019",
		[][3]any{{1, 1, "A1"}})
	srcArtist, srcAlbum := seedArtist(t, ctx, qtx, userID, libID, "Beta", "Beta LP", "2021",
		[][3]any{{1, 1, "B1"}})

	merged, err := mergeArtistIntoTx(ctx, qtx, dstArtist, srcArtist)
	require.NoError(t, err)
	require.True(t, merged)

	albums, err := qtx.ListAlbumsByArtist(ctx, dstArtist)
	require.NoError(t, err)
	require.Len(t, albums, 2) // both distinct albums now hang off dst

	// the moved album kept its identity, just re-pointed at dst.
	moved, err := qtx.GetAlbumByID(ctx, srcAlbum)
	require.NoError(t, err)
	require.Equal(t, dstArtist, moved.ArtistID)
}

func TestMergeArtistIntoTx_SrcGoneIsNoop(t *testing.T) {
	pool := mergeTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)
	qtx := sqlc.New(pool).WithTx(tx)

	userID, libID := seedUserAndMusicLib(t, ctx, qtx)
	dstArtist, _ := seedArtist(t, ctx, qtx, userID, libID, "Solo", "Solo LP", "2018", nil)

	merged, err := mergeArtistIntoTx(ctx, qtx, dstArtist, 999999999)
	require.NoError(t, err)
	require.False(t, merged) // nothing to merge → reported as no-op
}
