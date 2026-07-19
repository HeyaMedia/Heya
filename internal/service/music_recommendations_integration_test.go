package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Read-only production-shaped smoke test. It is opt-in because it needs a
// populated PostgreSQL database with pgvector and real music metadata.
func TestMusicRecommendationsIntegration(t *testing.T) {
	if os.Getenv("HEYA_RECOMMENDATION_INTEGRATION") != "1" {
		t.Skip("set HEYA_RECOMMENDATION_INTEGRATION=1 with HEYA_DATABASE_URL to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("HEYA_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	app := &App{db: pool}

	var userID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	// Ask for the API maximum; the service intentionally caps the useful slate
	// to four archetypes + six artist mixes so this also guards against an
	// accidental N+1/per-mix performance blowup.
	mixes, err := app.GenerateMixesForUser(ctx, userID, 20, 30, 0)
	if err != nil {
		t.Fatalf("generate mixes: %v", err)
	}
	if len(mixes) == 0 {
		t.Fatal("expected at least one recommendation mix")
	}
	if len(mixes) > 10 {
		t.Fatalf("recommendation slate should be bounded to 10 mixes, got %d", len(mixes))
	}
	seenSlugs := map[string]bool{}
	allMixTrackIDs := make([]int64, 0, len(mixes)*30)
	for _, mix := range mixes {
		t.Logf("mix %s (%s): %d tracks", mix.Slug, mix.Kind, len(mix.Tracks))
		if mix.Slug == "" || seenSlugs[mix.Slug] {
			t.Fatalf("missing or duplicate mix slug %q", mix.Slug)
		}
		seenSlugs[mix.Slug] = true
		if len(mix.Tracks) < 5 || len(mix.Tracks) > 30 {
			t.Fatalf("mix %q has %d tracks", mix.Slug, len(mix.Tracks))
		}
		seenTracks := map[int64]bool{}
		for _, track := range mix.Tracks {
			allMixTrackIDs = append(allMixTrackIDs, track.TrackID)
			if seenTracks[track.TrackID] {
				t.Fatalf("mix %q repeated track %d", mix.Slug, track.TrackID)
			}
			seenTracks[track.TrackID] = true
			var vetoed bool
			if err := pool.QueryRow(ctx, `SELECT
				EXISTS (SELECT 1 FROM user_track_ratings WHERE user_id=$1 AND track_id=$2 AND rating<=3)
				OR EXISTS (SELECT 1 FROM user_album_ratings uar JOIN tracks t ON t.album_id=uar.album_id WHERE uar.user_id=$1 AND t.id=$2 AND uar.rating<=3)
				OR EXISTS (SELECT 1 FROM user_artist_ratings uar JOIN albums al ON al.artist_id=uar.artist_id JOIN tracks t ON t.album_id=al.id WHERE uar.user_id=$1 AND t.id=$2 AND uar.rating<=3)`, userID, track.TrackID).Scan(&vetoed); err != nil {
				t.Fatal(err)
			}
			if vetoed {
				t.Fatalf("mix %q included vetoed track %d", mix.Slug, track.TrackID)
			}
		}
	}

	// Exercise the exact endless-queue continuation shape: several tracks from
	// the queue form a centroid, while everything already queued is excluded.
	seedCount := min(3, len(mixes[0].Tracks))
	blendSeeds := make([]RadioSeed, 0, seedCount)
	for _, track := range mixes[0].Tracks[:seedCount] {
		blendSeeds = append(blendSeeds, RadioSeed{Kind: "track", TrackID: track.TrackID})
	}
	continuation, err := app.BuildRadio(ctx, userID, RadioRequest{
		Seed:  RadioSeed{Kind: "track", TrackID: blendSeeds[0].TrackID},
		Seeds: blendSeeds, Limit: 20, ExcludeTrackIDs: allMixTrackIDs,
	})
	if err != nil {
		t.Fatalf("multi-seed queue continuation: %v", err)
	}
	if len(continuation.Tracks) < 5 {
		t.Fatalf("multi-seed queue continuation returned %d tracks", len(continuation.Tracks))
	}
	excluded := make(map[int64]bool, len(allMixTrackIDs))
	for _, id := range allMixTrackIDs {
		excluded[id] = true
	}
	for _, track := range continuation.Tracks {
		if excluded[track.TrackID] {
			t.Fatalf("multi-seed queue continuation repeated queued track %d", track.TrackID)
		}
	}

	// Exercise the no-ML fallback with a playable track that has no embedding.
	var coldTrackID int64
	if err := pool.QueryRow(ctx, `SELECT t.id
		FROM tracks t
		LEFT JOIN track_facets tf ON tf.track_id=t.id AND tf.track_embedding IS NOT NULL
		WHERE tf.track_id IS NULL
		  AND EXISTS (SELECT 1 FROM track_files atf JOIN library_files alf ON alf.id=atf.library_file_id WHERE atf.track_id=t.id AND alf.deleted_at IS NULL)
		ORDER BY t.id LIMIT 1`).Scan(&coldTrackID); err != nil {
		t.Fatal(err)
	}
	radio, err := app.BuildRadio(ctx, userID, RadioRequest{
		Seed: RadioSeed{Kind: "track", TrackID: coldTrackID}, Limit: 20,
	})
	if err != nil {
		t.Fatalf("metadata-fallback radio: %v", err)
	}
	if len(radio.Tracks) < 5 {
		t.Fatalf("metadata-fallback radio returned %d tracks", len(radio.Tracks))
	}

	// Exercise genre_affinity end-to-end against real album.genres/top_genres
	// data: an artist seed with a genre-tagged discography should still
	// return a full queue with the knob maxed out (drop-when-rich only fires
	// once enough overlapping candidates exist to still hit the limit).
	var genreArtistID int64
	if err := pool.QueryRow(ctx, `SELECT al.artist_id
		FROM albums al
		WHERE array_length(al.genres, 1) > 0
		  AND EXISTS (SELECT 1 FROM tracks t JOIN track_files atf ON atf.track_id = t.id
		              JOIN library_files alf ON alf.id = atf.library_file_id
		              WHERE t.album_id = al.id AND alf.deleted_at IS NULL)
		LIMIT 1`).Scan(&genreArtistID); err != nil {
		t.Fatal(err)
	}
	genreRadio, err := app.BuildRadio(ctx, userID, RadioRequest{
		Seed:          RadioSeed{Kind: "artist", ArtistID: genreArtistID},
		Limit:         20,
		GenreAffinity: 1.0,
	})
	if err != nil {
		t.Fatalf("genre_affinity=1 radio: %v", err)
	}
	if len(genreRadio.Tracks) == 0 {
		t.Fatal("genre_affinity=1 radio returned no tracks")
	}
}
