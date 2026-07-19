package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/metadatasync"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/stretchr/testify/require"
)

type stubTopTracksSource struct {
	projection heyametadata.ArtistTopTracksProjection
	err        error
}

func (s *stubTopTracksSource) ArtistTopTracksProjection(context.Context, string, ...heyametadata.ProviderCredentials) (heyametadata.ArtistTopTracksProjection, error) {
	return s.projection, s.err
}

func TestReconcileMetadataScopeWorkerPreservesAndCheckpointsSnapshots(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	suffix := uuid.NewString()
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "scope-worker-" + suffix, MediaType: sqlc.MediaTypeMusic,
		Paths:        []string{"/scope-worker-" + suffix},
		ScanInterval: pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMusic,
		Title: "Projection Artist " + suffix, SortTitle: "projection artist " + suffix,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: item.Title, SortName: item.SortTitle})
	require.NoError(t, err)
	entityID := uuid.New()
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID,
		EntityKind: "artist", SchemaVersion: 1, ProjectionVersion: 7,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_entity_bindings WHERE local_kind = 'artist' AND local_id = $1`, artist.ID)
		testutil.CleanupLibrary(t, pool, library.ID)
	})

	_, err = pool.Exec(ctx, `INSERT INTO artist_top_tracks (artist_id, rank, provider, provider_rank, title) VALUES ($1, 1, 'lastfm', 1, 'Previous')`, artist.ID)
	require.NoError(t, err)
	args := ReconcileMetadataScopeArgs{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID.String(),
		EntityKind: "artist", Scope: metadatasync.ArtistTopTracksScope, ProjectionVersion: 7,
	}
	source := &stubTopTracksSource{projection: heyametadata.ArtistTopTracksProjection{
		ProjectionVersion: 7,
		Entries: []metadata.TopTrackEntry{{
			Rank: 1, Provider: "lastfm", Title: "Broken", RecordingEntityID: "not-a-uuid",
		}},
	}}
	worker := &ReconcileMetadataScopeWorker{DB: pool, Source: source}

	// The insert fails after the transactional delete. The previous row must
	// survive and no checkpoint may claim that this snapshot was applied.
	err = worker.Work(ctx, &river.Job[ReconcileMetadataScopeArgs]{Args: args})
	require.Error(t, err)
	var titles []string
	err = pool.QueryRow(ctx, `SELECT coalesce(array_agg(title ORDER BY rank), '{}') FROM artist_top_tracks WHERE artist_id = $1`, artist.ID).Scan(&titles)
	require.NoError(t, err)
	require.Equal(t, []string{"Previous"}, titles)
	var states int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM metadata_projection_states WHERE local_kind = 'artist' AND local_id = $1 AND scope = 'top_tracks'`, artist.ID).Scan(&states))
	require.Zero(t, states)

	// A valid snapshot replaces the rows and checkpoints the canonical version.
	source.projection.Entries = []metadata.TopTrackEntry{
		{Rank: 2, Provider: "lastfm", Title: "Second"},
		{Rank: 1, Provider: "lastfm", Title: "First"},
	}
	require.NoError(t, worker.Work(ctx, &river.Job[ReconcileMetadataScopeArgs]{Args: args}))
	require.NoError(t, pool.QueryRow(ctx, `SELECT coalesce(array_agg(title ORDER BY rank), '{}') FROM artist_top_tracks WHERE artist_id = $1`, artist.ID).Scan(&titles))
	require.Equal(t, []string{"Second", "First"}, titles)
	state, err := q.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{LocalKind: "artist", LocalID: artist.ID, Scope: metadatasync.ArtistTopTracksScope})
	require.NoError(t, err)
	require.Equal(t, int64(7), state.ProjectionVersion)
	require.Equal(t, entityID, state.EntityID)

	// An authoritative empty snapshot is different from an error: clear the
	// rows and advance the durable checkpoint so backfill does not loop.
	source.projection.Entries = []metadata.TopTrackEntry{}
	source.projection.ProjectionVersion = 8
	args.ProjectionVersion = 8
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID,
		EntityKind: "artist", SchemaVersion: 1, ProjectionVersion: 8,
	})
	require.NoError(t, err)
	require.NoError(t, worker.Work(ctx, &river.Job[ReconcileMetadataScopeArgs]{Args: args}))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM artist_top_tracks WHERE artist_id = $1`, artist.ID).Scan(&states))
	require.Zero(t, states)
	state, err = q.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{LocalKind: "artist", LocalID: artist.ID, Scope: metadatasync.ArtistTopTracksScope})
	require.NoError(t, err)
	require.Equal(t, int64(8), state.ProjectionVersion)

	// A full-document refresh that started against version 7 must not replace
	// the already-applied version 8 snapshot when it finishes later.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	require.NoError(t, err)
	require.NoError(t, metadatasync.ReplaceArtistTopTracks(ctx, sqlc.New(tx), artist.ID, entityID, "artist", 7, []metadata.TopTrackEntry{{Rank: 1, Provider: "lastfm", Title: "Stale"}}))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM artist_top_tracks WHERE artist_id = $1`, artist.ID).Scan(&states))
	require.Zero(t, states)

	// A transport failure after that keeps the successful empty checkpoint.
	source.err = errors.New("upstream unavailable")
	args.ProjectionVersion = 9
	err = worker.Work(ctx, &river.Job[ReconcileMetadataScopeArgs]{Args: args})
	require.Error(t, err)
	require.Contains(t, fmt.Sprint(err), "upstream unavailable")
	state, err = q.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{LocalKind: "artist", LocalID: artist.ID, Scope: metadatasync.ArtistTopTracksScope})
	require.NoError(t, err)
	require.Equal(t, int64(8), state.ProjectionVersion)

	// A canonical 404 means the binding itself disappeared (an artist with no
	// ranking is a successful empty 200). Recover through the existing full
	// enrichment path so discovery can establish the replacement UUID.
	insertClient, err := NewInsertClient(pool)
	require.NoError(t, err)
	source.err = &heyametadata.APIError{Operation: "read canonical artist top tracks", Status: http.StatusNotFound}
	recoveryCtx := rivertest.WorkContext(ctx, insertClient)
	require.NoError(t, worker.Work(recoveryCtx, &river.Job[ReconcileMetadataScopeArgs]{Args: args}))
	var repairJobID int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id FROM river_job
		WHERE kind = 'enrich_media_item'
		  AND NULLIF(args->>'item_id', '')::bigint = $1
		  AND args->>'source' = 'metadata_scope_rebind'
		ORDER BY id DESC LIMIT 1`, item.ID).Scan(&repairJobID))
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, repairJobID) })
}

func TestReconcileMetadataScopeWorkerDoesNotPromoteFetchedSnapshotToNewBindingVersion(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	suffix := uuid.NewString()
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "scope-race-" + suffix, MediaType: sqlc.MediaTypeMusic,
		Paths:        []string{"/scope-race-" + suffix},
		ScanInterval: pgtype.Interval{Microseconds: 3_600_000_000, Valid: true},
		CreatedBy:    userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMusic,
		Title: "Projection Race " + suffix, SortTitle: "projection race " + suffix,
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: item.Title, SortName: item.SortTitle})
	require.NoError(t, err)
	entityID := uuid.New()
	_, err = q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID,
		EntityKind: "artist", SchemaVersion: 1, ProjectionVersion: 8,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_entity_bindings WHERE local_kind = 'artist' AND local_id = $1`, artist.ID)
		testutil.CleanupLibrary(t, pool, library.ID)
	})

	source := &stubTopTracksSource{projection: heyametadata.ArtistTopTracksProjection{
		ProjectionVersion: 8,
		Entries:           []metadata.TopTrackEntry{{Rank: 1, Provider: "lastfm", Title: "Version Eight"}},
	}}
	worker := &ReconcileMetadataScopeWorker{
		DB: pool, Source: source,
		BeforeStoreTransaction: func() {
			_, updateErr := q.UpsertMetadataEntityBinding(ctx, sqlc.UpsertMetadataEntityBindingParams{
				LocalKind: "artist", LocalID: artist.ID, EntityID: entityID,
				EntityKind: "artist", SchemaVersion: 1, ProjectionVersion: 10,
			})
			require.NoError(t, updateErr)
		},
	}
	args := ReconcileMetadataScopeArgs{
		LocalKind: "artist", LocalID: artist.ID, EntityID: entityID.String(), EntityKind: "artist",
		Scope: metadatasync.ArtistTopTracksScope, ProjectionVersion: 8,
	}
	require.NoError(t, worker.Work(ctx, &river.Job[ReconcileMetadataScopeArgs]{Args: args}))

	state, err := q.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{
		LocalKind: "artist", LocalID: artist.ID, Scope: metadatasync.ArtistTopTracksScope,
	})
	require.NoError(t, err)
	require.Equal(t, int64(8), state.ProjectionVersion, "v8 payload must never be checkpointed as the concurrently advanced v10 artist binding")
	binding, err := q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: "artist", LocalID: artist.ID})
	require.NoError(t, err)
	require.Equal(t, int64(10), binding.ProjectionVersion)
	var title string
	require.NoError(t, pool.QueryRow(ctx, `SELECT title FROM artist_top_tracks WHERE artist_id=$1`, artist.ID).Scan(&title))
	require.Equal(t, "Version Eight", title)
}
