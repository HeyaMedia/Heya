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
	_, err = q.UpsertScanRunArtifact(ctx, sqlc.UpsertScanRunArtifactParams{
		ScanRunID:     scanRun.ID,
		Kind:          "search_result",
		ScopeKey:      scopeKey,
		SchemaVersion: 1,
		Data:          []byte(`{"stage":"search"}`),
	})
	require.NoError(t, err)

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

	deleted, err := cleanupOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute))
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted.EntitiesDeleted)
	require.EqualValues(t, 1, deleted.EntityArtifactsDeleted)
	require.EqualValues(t, 1, deleted.ScanRunArtifactsDeleted)

	_, err = q.GetScannerEntity(ctx, entity.ID)
	require.Error(t, err)
	_, err = q.GetScannerEntityArtifact(ctx, entityArtifact.ID)
	require.Error(t, err)
	_, err = q.GetScanRunArtifact(ctx, sqlc.GetScanRunArtifactParams{ScanRunID: scanRun.ID, Kind: "search_result", ScopeKey: scopeKey})
	require.Error(t, err)
}
