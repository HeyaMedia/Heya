package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
		EntityID:           entity.ID,
		Stage:              "search_result",
		SchemaVersion:      1,
		ScanRunID:          pgtype.Int8{Int64: scanRun.ID, Valid: true},
		Data:               []byte(`{"stage":"search"}`),
		PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET search_artifact_id = $1, updated_at = now() - interval '30 minutes' WHERE id = $2`, entityArtifact.ID, entity.ID)
	require.NoError(t, err)

	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), lib.ID)
	require.NoError(t, err)
	require.Len(t, orphaned, 1)
	require.Equal(t, entity.ID, orphaned[0].ID)
	require.Equal(t, lib.ID, orphaned[0].LibraryID)
	require.Equal(t, scopePaths, orphaned[0].ScopePaths, "scope paths surface so the pruner can requeue the work")

	deleted, err := cleanupOrphanedInFlightScannerEntities(ctx, pool, orphaned)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted.EntitiesDeleted)
	require.EqualValues(t, 1, deleted.EntityArtifactsDeleted)
	require.Equal(t, []int64{entity.ID}, deleted.EntityIDs)

	_, err = q.GetScannerEntity(ctx, entity.ID)
	require.Error(t, err)
	_, err = q.GetScannerEntityArtifact(ctx, entityArtifact.ID)
	require.Error(t, err)
}

func TestListOrphanedInFlightScannerEntitiesCoversEveryRecoverableStage(t *testing.T) {
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

	statusesToRecover := []string{
		"fetched", "applying", "stale", "error", "metadata_error", "apply_error", "failed",
	}
	for _, status := range statusesToRecover {
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

	statuses := map[string]bool{}
	for _, e := range orphaned {
		if e.LibraryID == lib.ID {
			statuses[e.Status] = true
		}
	}
	for _, status := range statusesToRecover {
		require.Truef(t, statuses[status], "status %q has no live River retry and must be recovered", status)
	}
}

func TestDeadStaleReplacementIsRequeuedByOrphanReconciliation(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "dead-stale-replacement", MediaType: sqlc.MediaTypeMovie,
		Paths: []string{"/media/movies"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	scope := "/media/movies/Stale (2026)"
	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope:stale",
		ScopePaths: []string{scope}, IdentityKey: "movie:stale", Title: "Stale",
		Status: "stale", ErrorMessage: "source set changed", Data: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET updated_at=now()-interval '30 minutes' WHERE id=$1`, entity.ID)
	require.NoError(t, err)
	// The corrective process job has no scanner_entity_id because analysis may
	// fan into a different identity set. Simulate it exhausting before it can
	// persist analysis; only orphan scope recovery can now move the unit.
	var deadJobID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO river_job(kind,queue,args,max_attempts,attempt,state,finalized_at)
		VALUES('process_library_scan','process_library_scan',$1,3,3,'discarded',now())
		RETURNING id`, []byte(fmt.Sprintf(`{"library_id":%d,"scope_paths":[%q],"force":true}`, lib.ID, scope))).Scan(&deadJobID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id=$1`, deadJobID) })

	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), lib.ID)
	require.NoError(t, err)
	require.Len(t, orphaned, 1)
	require.Equal(t, "stale", orphaned[0].Status)
	var replacement ProcessLibraryScanArgs
	requeued, counts, err := requeueThenCleanupOrphanedScannerEntities(ctx, pool, orphaned, func(_ context.Context, args ProcessLibraryScanArgs) error {
		replacement = args
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, requeued)
	require.EqualValues(t, 1, counts.EntitiesDeleted)
	require.True(t, replacement.Force)
	require.Equal(t, lib.ID, replacement.LibraryID)
	require.Equal(t, []string{scope}, replacement.ScopePaths)
}

func TestOrphanCleanupCannotDeleteReplacementAnalysisGeneration(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "orphan-generation-cas", MediaType: sqlc.MediaTypeMovie,
		Paths: []string{"/media/movies"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	params := sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope:race",
		ScopePaths: []string{"/media/movies/Race (2026)"}, IdentityKey: "movie:race",
		Title: "Race", Status: "matched", Data: []byte("{}"),
	}
	old, err := q.UpsertScannerEntity(ctx, params)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET updated_at=now()-interval '30 minutes' WHERE id=$1`, old.ID)
	require.NoError(t, err)
	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), lib.ID)
	require.NoError(t, err)
	require.Len(t, orphaned, 1)

	var replacement sqlc.ScannerEntity
	requeued, counts, err := requeueThenCleanupOrphanedScannerEntities(ctx, pool, orphaned, func(context.Context, ProcessLibraryScanArgs) error {
		params.Status = "discovered"
		replacement, err = q.UpsertScannerEntity(ctx, params)
		if err != nil {
			return err
		}
		artifact, createErr := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
			EntityID: replacement.ID, Stage: "analysis_result", SchemaVersion: 1,
			Data: []byte(`{"replacement":true}`), PipelineGeneration: replacement.PipelineGeneration,
		})
		if createErr != nil {
			return createErr
		}
		_, attachErr := q.AttachScannerEntityAnalysisArtifact(ctx, sqlc.AttachScannerEntityAnalysisArtifactParams{
			AnalysisArtifactID: pgtype.Int8{Int64: artifact.ID, Valid: true},
			EntityID:           replacement.ID, PipelineGeneration: replacement.PipelineGeneration,
		})
		return attachErr
	})
	require.NoError(t, err)
	require.Equal(t, 1, requeued)
	require.Zero(t, counts.EntitiesDeleted, "cleanup snapshot must not delete a row reused by replacement analysis")
	current, err := q.GetScannerEntity(ctx, old.ID)
	require.NoError(t, err)
	require.Greater(t, current.PipelineGeneration, orphaned[0].PipelineGeneration)
	require.Equal(t, replacement.PipelineGeneration, current.PipelineGeneration)
	require.True(t, current.AnalysisArtifactID.Valid, "replacement analysis handoff was stranded")
}

func TestOrphanedScannerRequeueArgsSkipCancelledEntities(t *testing.T) {
	args := orphanedScannerRequeueArgs([]orphanedScannerEntity{
		{ID: 1, LibraryID: 5, ScopePaths: []string{"/m/A"}, Cancelled: true},
		{ID: 2, LibraryID: 5, ScopePaths: []string{"/m/B"}},
	})
	require.Len(t, args, 1, "user-cancelled entities are cleaned up but never resurrected")
	require.Equal(t, []string{"/m/B"}, args[0].ScopePaths)
}

func TestCancelledJobMustMatchCurrentArtifactGeneration(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "cancelled-generation-guard", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/media/music"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	params := sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope:ado",
		ScopePaths: []string{"/media/music/Ado"}, IdentityKey: "artist:ado",
		Title: "Ado", Status: "discovered", Data: []byte("{}"),
	}
	first, err := q.UpsertScannerEntity(ctx, params)
	require.NoError(t, err)
	oldArtifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: first.ID, Stage: "analysis_result", SchemaVersion: 1,
		Data: []byte("{}"), PipelineGeneration: first.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = q.AttachScannerEntityAnalysisArtifact(ctx, sqlc.AttachScannerEntityAnalysisArtifactParams{
		AnalysisArtifactID: pgtype.Int8{Int64: oldArtifact.ID, Valid: true},
		EntityID:           first.ID, PipelineGeneration: first.PipelineGeneration,
	})
	require.NoError(t, err)
	var jobID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO river_job (kind, queue, args, max_attempts, state, finalized_at)
		VALUES ('search_metadata', 'search_metadata', $1, 3, 'cancelled', now())
		RETURNING id`, []byte(fmt.Sprintf(`{"scanner_entity_id":%d,"analysis_artifact_id":%d}`, first.ID, oldArtifact.ID))).Scan(&jobID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, jobID) })

	current, err := q.UpsertScannerEntity(ctx, params)
	require.NoError(t, err)
	require.Greater(t, current.PipelineGeneration, first.PipelineGeneration)
	currentArtifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: current.ID, Stage: "analysis_result", SchemaVersion: 1,
		Data: []byte("{}"), PipelineGeneration: current.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = q.AttachScannerEntityAnalysisArtifact(ctx, sqlc.AttachScannerEntityAnalysisArtifactParams{
		AnalysisArtifactID: pgtype.Int8{Int64: currentArtifact.ID, Valid: true},
		EntityID:           current.ID, PipelineGeneration: current.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET updated_at = now() - interval '30 minutes' WHERE id = $1`, current.ID)
	require.NoError(t, err)

	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), lib.ID)
	require.NoError(t, err)
	require.Len(t, orphaned, 1)
	require.False(t, orphaned[0].Cancelled, "a cancelled generation-1 job must not cancel generation-2 work")
}

func TestOrphanCleanupRetainsEveryEntityWhenDurableRequeueFails(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "orphan-requeue-failure", MediaType: sqlc.MediaTypeMovie,
		Paths: []string{"/media/movies"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	var candidates []orphanedScannerEntity
	for index, scope := range []string{"/media/movies/A", "/media/movies/B"} {
		entity, createErr := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
			LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: fmt.Sprintf("scope:%d", index),
			ScopePaths: []string{scope}, IdentityKey: fmt.Sprintf("movie:%d", index),
			Title: filepath.Base(scope), Status: "matched", Data: []byte("{}"),
		})
		require.NoError(t, createErr)
		candidates = append(candidates, orphanedScannerEntity{ID: entity.ID, LibraryID: lib.ID, ScopePaths: []string{scope}})
	}

	enqueues := 0
	requeued, counts, err := requeueThenCleanupOrphanedScannerEntities(ctx, pool, candidates, func(context.Context, ProcessLibraryScanArgs) error {
		enqueues++
		if enqueues == 2 {
			return errors.New("injected River insert failure")
		}
		return nil
	})
	require.ErrorContains(t, err, "injected River insert failure")
	require.Equal(t, 1, requeued)
	require.Zero(t, counts.EntitiesDeleted)
	for _, candidate := range candidates {
		_, getErr := q.GetScannerEntity(ctx, candidate.ID)
		require.NoError(t, getErr, "no orphan may be deleted until every required scope is durably queued")
	}
}

func TestScannerCleanupRechecksActiveRiverJobsBeforeDeletingEntity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "active-scanner-job-cleanup-guard", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/media/music"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope:active-job",
		ScopePaths: []string{"/media/music/Active Job"}, IdentityKey: "artist:active-job",
		Title: "Active Job", Status: "matched", Data: []byte("{}"),
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET updated_at = now() - interval '72 hours' WHERE id = $1`, entity.ID)
	require.NoError(t, err)

	var jobID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO river_job (kind, queue, args, max_attempts, state)
		VALUES ('future_scanner_stage', 'future_scanner_stage', $1, 5, 'available')
		RETURNING id`, []byte(fmt.Sprintf(`{"scanner_entity_id":%d}`, entity.ID))).Scan(&jobID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, jobID) })

	orphaned, err := listOrphanedInFlightScannerEntities(ctx, pool, time.Now().Add(-15*time.Minute), lib.ID)
	require.NoError(t, err)
	require.Empty(t, orphaned, "all active River kinds guard an entity, including future stages")

	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	counts, err := cleanupOrphanedInFlightScannerEntities(ctx, pool, []orphanedScannerEntity{{
		ID: entity.ID, LibraryID: lib.ID, PipelineGeneration: entity.PipelineGeneration,
		Status: entity.Status, UpdatedAt: entity.UpdatedAt.Time,
	}})
	require.NoError(t, err)
	require.Zero(t, counts.EntitiesDeleted, "the delete statement must recheck instead of trusting a stale candidate list")
	require.Empty(t, counts.EntityIDs, "a guarded entity must not be requeued from the stale candidate snapshot")
	counts, err = cleanupStaleInFlightScannerEntitiesOlderThan(ctx, pool, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	require.Zero(t, counts.EntitiesDeleted)
	_, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
}

func TestDeletedOrphanedScannerEntitiesOnlyReturnsCommittedDeletes(t *testing.T) {
	orphaned := []orphanedScannerEntity{
		{ID: 10, LibraryID: 1, ScopePaths: []string{"/music/A"}},
		{ID: 20, LibraryID: 1, ScopePaths: []string{"/music/B"}},
	}
	got := deletedOrphanedScannerEntities(orphaned, []int64{20})
	require.Equal(t, []orphanedScannerEntity{orphaned[1]}, got)
}

func TestSupersededArtifactCleanupHandlesSameGenerationAndContinuationGuard(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "same-generation-artifact-cleanup", MediaType: sqlc.MediaTypeMovie,
		Paths: []string{"/media/movies"}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope:retry",
		ScopePaths: []string{"/media/movies/Retry (2024)"}, IdentityKey: "title_year:retry|2024",
		Title: "Retry", Year: "2024", Status: "needs_review", Data: []byte("{}"),
	})
	require.NoError(t, err)
	analysis, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "analysis_result", SchemaVersion: 1, Data: []byte("{}"),
		PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	oldSearch, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "search_result", SchemaVersion: 1, Data: []byte("{}"),
		PipelineGeneration: entity.PipelineGeneration, SourceArtifactID: pgtype.Int8{Int64: analysis.ID, Valid: true},
	})
	require.NoError(t, err)
	currentSearch, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "search_result", SchemaVersion: 1, Data: []byte("{}"),
		PipelineGeneration: entity.PipelineGeneration, SourceArtifactID: pgtype.Int8{Int64: analysis.ID, Valid: true},
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE scanner_entities
		SET analysis_artifact_id = $1, search_artifact_id = $2
		WHERE id = $3`, analysis.ID, currentSearch.ID, entity.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entity_artifacts SET created_at = now() - interval '72 hours' WHERE id = $1`, oldSearch.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO scanner_metadata_continuations
			(kind, library_id, scanner_entity_id, artifact_id, args, next_attempt_at)
		VALUES ('fetch_metadata', $1, $2, $3, '{}'::jsonb, now() + interval '1 hour')`,
		lib.ID, entity.ID, oldSearch.ID)
	require.NoError(t, err)

	deleted, err := cleanupSupersededScannerEntityArtifactsOlderThan(ctx, pool, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	_ = deleted // the shared test database can contain superseded artifacts from other libraries
	_, err = q.GetScannerEntityArtifact(ctx, oldSearch.ID)
	require.NoError(t, err, "a parked continuation owns its artifact until it is promoted or removed")

	_, err = pool.Exec(ctx, `DELETE FROM scanner_metadata_continuations WHERE scanner_entity_id = $1`, entity.ID)
	require.NoError(t, err)
	deleted, err = cleanupSupersededScannerEntityArtifactsOlderThan(ctx, pool, time.Now().Add(-48*time.Hour))
	require.NoError(t, err)
	require.GreaterOrEqual(t, deleted, int64(1), "same-generation retry artifacts are superseded even without an older generation")
	_, err = q.GetScannerEntityArtifact(ctx, oldSearch.ID)
	require.Error(t, err)
	_, err = q.GetScannerEntityArtifact(ctx, currentSearch.ID)
	require.NoError(t, err)
	_, err = q.GetScannerEntityArtifact(ctx, analysis.ID)
	require.NoError(t, err)
}
