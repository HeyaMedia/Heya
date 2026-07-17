package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/karbowiak/heya/internal/textembed"
	"github.com/pgvector/pgvector-go"
)

func TestMusicMetadataSeedFindsUnownedCanonicalRecording(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	app := &App{db: pool}

	seedID, suggestionID := uuid.New(), uuid.New()
	localTrackID := -time.Now().UnixNano()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM metadata_entity_bindings WHERE local_kind = 'track' AND local_id = $1`, localTrackID)
		_, _ = pool.Exec(ctx, `DELETE FROM music_catalog_recordings WHERE recording_entity_id = ANY($1::uuid[])`, []uuid.UUID{seedID, suggestionID})
	})

	insertRecording := func(id uuid.UUID, title, artist, url string) {
		_, err := pool.Exec(ctx, `
			INSERT INTO music_catalog_recordings
			  (recording_entity_id, title, artist_name, provider_url, genres, tags, moods, instrumentation, vocal_characteristics)
			VALUES ($1, $2, $3, $4, ARRAY['J-Rock'], ARRAY['Japanese'], ARRAY['aggressive'], ARRAY['guitar'], ARRAY['lead vocals'])`,
			id, title, artist, url)
		if err != nil {
			t.Fatal(err)
		}
		vector := make([]float32, textembed.Dim)
		vector[0] = 1
		if _, err := pool.Exec(ctx, `
			INSERT INTO music_recording_facets (recording_entity_id, text_embedding, embedder_version, doc_hash)
			VALUES ($1, $2, $3, 'test')`, id, pgvector.NewVector(vector), int32(textembed.Version)); err != nil {
			t.Fatal(err)
		}
	}
	insertRecording(seedID, "Seed", "Seed Artist", "")
	insertRecording(suggestionID, "Unowned Match", "Another Artist", "https://example.test/track")
	if _, err := pool.Exec(ctx, `
		INSERT INTO metadata_entity_bindings (local_kind, local_id, entity_id, entity_kind)
		VALUES ('track', $1, $2, 'recording')`, localTrackID, seedID); err != nil {
		t.Fatal(err)
	}

	centroid, seedFacets, seedIDs, err := app.musicMetadataForTracks(ctx, []int64{localTrackID})
	if err != nil {
		t.Fatal(err)
	}
	if len(centroid.Slice()) != textembed.Dim || len(seedIDs) != 1 || seedIDs[0] != seedID {
		t.Fatalf("seed metadata was not resolved: dimensions=%d ids=%v", len(centroid.Slice()), seedIDs)
	}
	suggestions, err := app.unownedMetadataSuggestions(ctx, centroid, seedFacets, seedIDs, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) == 0 || suggestions[0].RecordingEntityID != suggestionID.String() {
		t.Fatalf("unowned suggestions = %#v", suggestions)
	}
	if suggestions[0].Reason != "Shared: aggressive, guitar, lead vocals" {
		t.Fatalf("suggestion reason = %q", suggestions[0].Reason)
	}
}
