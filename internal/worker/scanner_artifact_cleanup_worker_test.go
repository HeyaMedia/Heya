package worker

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestCleanupOrphanedInFlightScannerEntitiesDeletesMatchedEntityWithoutActiveJob(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "orphaned-scanner-entity-cleanup-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/media/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	scopeKey := "scope:orphaned-artist"
	scopePaths := []string{"/media/music/Orphaned Artist"}
	scanRun, err := q.CreateScanRun(ctx, sqlc.CreateScanRunParams{
		LibraryID:      lib.ID,
		MediaType:      lib.MediaType,
		ScannerVersion: "scanner-test",
		Mode:           "search",
		Status:         "running",
		Summary:        []byte("{}"),
	})
	require.NoError(t, err)
	require.NoError(t, q.FinishScanRun(ctx, sqlc.FinishScanRunParams{
		ID:           scanRun.ID,
		Status:       "complete",
		Summary:      []byte("{}"),
		ErrorMessage: "",
	}))

	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID:        lib.ID,
		MediaType:        lib.MediaType,
		ScopeKey:         scopeKey,
		ScopePaths:       scopePaths,
		IdentityKey:      "artist:orphaned",
		Title:            "Orphaned Artist",
		ProviderID:       "heya:artist:mbid:orphaned",
		Status:           "matched",
		SearchScanRunID:  pgtype.Int8{Int64: scanRun.ID, Valid: true},
		SearchArtifactID: pgtype.Int8{},
		ErrorMessage:     "",
		Data:             []byte("{}"),
	})
	require.NoError(t, err)
	entityArtifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID:      entity.ID,
		Stage:         "search",
		SchemaVersion: 1,
		ScanRunID:     pgtype.Int8{Int64: scanRun.ID, Valid: true},
		Data:          []byte(`{"stage":"search"}`),
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET search_artifact_id = $1, updated_at = now() - interval '30 minutes' WHERE id = $2`, entityArtifact.ID, entity.ID)
	require.NoError(t, err)

	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), 0)
	require.NoError(t, err)
	require.Len(t, orphaned, 1)
	require.Equal(t, entity.ID, orphaned[0].ID)
	require.Equal(t, lib.ID, orphaned[0].LibraryID)
	require.Equal(t, scopePaths, orphaned[0].ScopePaths, "scope paths surface so the pruner can requeue the work")

	deleted, err := cleanupOrphanedInFlightScannerEntities(ctx, pool, orphaned)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted.EntitiesDeleted)
	require.EqualValues(t, 1, deleted.EntityArtifactsDeleted)

	_, err = q.GetScannerEntity(ctx, entity.ID)
	require.Error(t, err)
	_, err = q.GetScannerEntityArtifact(ctx, entityArtifact.ID)
	require.Error(t, err)
}

func TestListOrphanedInFlightScannerEntitiesCoversApplyStates(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "orphaned-apply-state-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	for _, status := range []string{"fetched", "applying"} {
		entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
			LibraryID:   lib.ID,
			MediaType:   lib.MediaType,
			ScopeKey:    "scope:" + status,
			ScopePaths:  []string{"/media/movies/Stuck (2020)"},
			IdentityKey: "movie:" + status,
			Title:       "Stuck",
			Status:      status,
			Data:        []byte("{}"),
		})
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE scanner_entities SET updated_at = now() - interval '30 minutes' WHERE id = $1`, entity.ID)
		require.NoError(t, err)
	}

	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), 0)
	require.NoError(t, err)

	statuses := 0
	for _, e := range orphaned {
		if e.LibraryID == lib.ID {
			statuses++
		}
	}
	require.Equal(t, 2, statuses, "entities stuck in fetched/applying with no live job are orphans too")
}

func TestOrphanedScannerRequeueArgsSkipCancelledEntities(t *testing.T) {
	args := orphanedScannerRequeueArgs([]orphanedScannerEntity{
		{ID: 1, LibraryID: 5, ScopePaths: []string{"/m/A"}, Cancelled: true},
		{ID: 2, LibraryID: 5, ScopePaths: []string{"/m/B"}},
	})
	require.Len(t, args, 1, "user-cancelled entities are cleaned up but never resurrected")
	require.Equal(t, []string{"/m/B"}, args[0].ScopePaths)
}
