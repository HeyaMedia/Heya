package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

type richMetadataCallCounter struct {
	MatchService
	calls int
}

func (m *richMetadataCallCounter) StoreRichMetadata(context.Context, int64, *metadata.MediaDetail) error {
	m.calls++
	return nil
}

func (m *richMetadataCallCounter) StoreRichMetadataTx(context.Context, pgx.Tx, int64, *metadata.MediaDetail) error {
	m.calls++
	return nil
}

func TestScannerWorkerErrorSnoozesDeferredMetadataWork(t *testing.T) {
	err := scannerWorkerError(&metadata.DeferredWorkError{Operation: "test discovery", RetryAfter: 30 * time.Second})
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, err, &snooze)
	require.Equal(t, 30*time.Second, snooze.Duration)
}

func TestScannerEntityExecutionLockSerializesDuplicateApplyWorkers(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	first, acquired, err := tryScannerEntityExecutionLock(ctx, pool, "apply", 880001)
	require.NoError(t, err)
	require.True(t, acquired)

	second, acquired, err := tryScannerEntityExecutionLock(ctx, pool, "apply", 880001)
	require.NoError(t, err)
	require.False(t, acquired, "a duplicate worker must not enter apply concurrently")
	require.Nil(t, second)

	releaseScannerEntityExecutionLock(first)
	third, acquired, err := tryScannerEntityExecutionLock(ctx, pool, "apply", 880001)
	require.NoError(t, err)
	require.True(t, acquired, "a crashed/completed worker releases the database lock for retry")
	releaseScannerEntityExecutionLock(third)
}

func TestSearchToFetchHandoffRollsBackSearchCheckpointWhenEnqueueFails(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	root := t.TempDir()
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "search-fetch-atomic-handoff", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	key := "title_year:atomic handoff|2026"
	result := scanner.Result{
		Inventory: scanner.Inventory{Roots: []scanner.InventoryRoot{{Root: root, Files: []scanner.InventoryFile{{
			Root: root, Path: filepath.Join(root, "Atomic Handoff.mkv"), RelPath: "Atomic Handoff.mkv", Class: scanner.ClassPrimaryMedia,
		}}}}},
		MovieMatches: []scanner.MovieMatch{{Key: key, Title: "Atomic Handoff", Year: "2026", Files: []string{"Atomic Handoff.mkv"}}},
		MovieSearch: []scanner.MovieSearchMatch{{
			Key: key, Accepted: true, ProviderID: "heya:movie:atomic", Title: "Atomic Handoff", Year: "2026", Confidence: 1,
		}},
	}
	opts := scanner.Options{ScopePaths: []string{root}}
	analysis, err := scanner.PersistScannerAnalysisEntities(ctx, pool, lib, opts, result)
	require.NoError(t, err)
	require.Len(t, analysis, 1)

	sentinel := errors.New("injected River insert failure")
	_, current, err := scanner.PersistScannerSearchEntityWithHandoff(
		ctx, pool, lib, opts, analysis[0].Entity.ID, analysis[0].Artifact.ID, result, 0,
		func(context.Context, pgx.Tx, scanner.ScannerEntityRef) error { return sentinel },
	)
	require.ErrorIs(t, err, sentinel)
	require.False(t, current)
	entity, err := q.GetScannerEntity(ctx, analysis[0].Entity.ID)
	require.NoError(t, err)
	require.Equal(t, "discovered", entity.Status)
	require.False(t, entity.SearchArtifactID.Valid, "search checkpoint must roll back with the failed fetch insert")

	rc, err := NewInsertClient(pool)
	require.NoError(t, err)
	ref, current, err := scanner.PersistScannerSearchEntityWithHandoff(
		ctx, pool, lib, opts, analysis[0].Entity.ID, analysis[0].Artifact.ID, result, 0,
		func(handoffCtx context.Context, tx pgx.Tx, ref scanner.ScannerEntityRef) error {
			return enqueueFetchLibraryMetadataTx(handoffCtx, rc, tx, FetchLibraryMetadataArgs{
				LibraryID: lib.ID, MediaType: lib.MediaType, ScopePaths: []string{root},
				ScannerEntityID: ref.Entity.ID, SearchArtifactID: ref.Artifact.ID,
			}, PriorityScan, "")
		},
	)
	require.NoError(t, err)
	require.True(t, current)
	require.NotZero(t, ref.Artifact.ID)
	entity, err = q.GetScannerEntity(ctx, analysis[0].Entity.ID)
	require.NoError(t, err)
	require.Equal(t, ref.Artifact.ID, entity.SearchArtifactID.Int64)
	var jobs int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM river_job
		WHERE kind = 'fetch_metadata'
		  AND (args->>'scanner_entity_id')::bigint = $1
		  AND (args->>'search_artifact_id')::bigint = $2
	`, entity.ID, ref.Artifact.ID).Scan(&jobs))
	require.Equal(t, 1, jobs)

	current, err = scanner.BeginScannerEntityFetch(ctx, pool, entity.ID, ref.Artifact.ID)
	require.NoError(t, err)
	require.True(t, current)
	fetched := result
	fetched.MovieMetadata = []scanner.MovieFetchPreview{{
		Key: key, ProviderID: "heya:movie:atomic", Detail: &metadata.MediaDetail{Title: "Atomic Handoff", Year: "2026"},
	}}
	_, current, err = scanner.PersistScannerFetchEntityWithHandoff(
		ctx, pool, entity.ID, ref.Artifact.ID, fetched, 0,
		func(context.Context, pgx.Tx, sqlc.ScannerEntityArtifact, scanner.Result) error { return sentinel },
	)
	require.ErrorIs(t, err, sentinel)
	require.False(t, current)
	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "fetching", entity.Status)
	require.False(t, entity.MetadataArtifactID.Valid, "fetch checkpoint must roll back with the failed apply insert")

	metadataArtifact, current, err := scanner.PersistScannerFetchEntityWithHandoff(
		ctx, pool, entity.ID, ref.Artifact.ID, fetched, 0,
		func(handoffCtx context.Context, tx pgx.Tx, artifact sqlc.ScannerEntityArtifact, _ scanner.Result) error {
			return enqueueApplyLibraryScanTx(handoffCtx, rc, tx, ApplyLibraryScanArgs{
				LibraryID: lib.ID, MediaType: lib.MediaType, ScopePaths: []string{root},
				ScannerEntityID: entity.ID, MetadataArtifactID: artifact.ID,
			}, PriorityScan, "")
		},
	)
	require.NoError(t, err)
	require.True(t, current)
	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, metadataArtifact.ID, entity.MetadataArtifactID.Int64)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM river_job
		WHERE kind = 'apply_metadata'
		  AND (args->>'scanner_entity_id')::bigint = $1
		  AND (args->>'metadata_artifact_id')::bigint = $2
	`, entity.ID, metadataArtifact.ID).Scan(&jobs))
	require.Equal(t, 1, jobs)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind IN ('fetch_metadata', 'apply_metadata') AND (args->>'scanner_entity_id')::bigint = $1`, entity.ID)
	})
}

func TestStaleArtifactRecoveryRollsBackStateWhenReanalysisInsertFails(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "stale-artifact-atomic-recovery", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/media/stale"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope", ScopePaths: []string{"/media/stale/Movie"},
		IdentityKey: "title_year:stale|2026", Title: "Stale", Year: "2026", Status: "discovered", Data: []byte("{}"),
	})
	require.NoError(t, err)
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "fetch_result", SchemaVersion: 1, Data: []byte("{}"), PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET status = 'applying', metadata_artifact_id = $2 WHERE id = $1`, entity.ID, artifact.ID)
	require.NoError(t, err)

	sentinel := errors.New("injected process_scan insert failure")
	handled, err := enqueueStaleScannerArtifactReanalysisWithInsert(
		ctx, nil, pool, lib, entity.ID, artifact.ID, entity.ScopePaths, "", "", &scanner.ArtifactReplayError{Reason: "source changed"},
		func(context.Context, pgx.Tx, ProcessLibraryScanArgs) error { return sentinel },
	)
	require.True(t, handled)
	require.ErrorIs(t, err, sentinel)
	current, err := q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "applying", current.Status, "stale transition must roll back when replacement work is not durable")
	require.Empty(t, current.ErrorMessage)
	var jobs int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'process_scan' AND (args->>'library_id')::bigint = $1`, lib.ID).Scan(&jobs))
	require.Zero(t, jobs)

	rc, err := NewInsertClient(pool)
	require.NoError(t, err)
	handled, err = enqueueStaleScannerArtifactReanalysis(
		ctx, rc, pool, lib, entity.ID, artifact.ID, entity.ScopePaths, "", "", &scanner.ArtifactReplayError{Reason: "source changed"},
	)
	require.True(t, handled)
	require.NoError(t, err)
	current, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "stale", current.Status)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'process_scan' AND (args->>'library_id')::bigint = $1`, lib.ID).Scan(&jobs))
	require.Equal(t, 1, jobs)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = 'process_scan' AND (args->>'library_id')::bigint = $1`, lib.ID)
	})
}

func TestApplyErrorCheckpointCanRetryCoreApply(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "retry-apply-error", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/media/retry-apply"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope", ScopePaths: []string{"/media/retry-apply/Movie"},
		IdentityKey: "movie:retry", Title: "Retry", Status: "discovered", Data: []byte("{}"),
	})
	require.NoError(t, err)
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "fetch_result", SchemaVersion: 1, Data: []byte("{}"), PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET status = 'apply_error', metadata_artifact_id = $2 WHERE id = $1`, entity.ID, artifact.ID)
	require.NoError(t, err)

	current, err := scanner.BeginScannerEntityApply(ctx, pool, entity.ID, artifact.ID)
	require.NoError(t, err)
	require.True(t, current, "River's retry after an apply failure must re-enter core apply")
	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "applying", entity.Status)
}

func TestApplyFanoutJobsAndTerminalStatusCommitAtomically(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "atomic-apply-fanout", MediaType: sqlc.MediaTypeMovie, Paths: []string{"/media/atomic-apply"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope", ScopePaths: []string{"/media/atomic-apply/Movie"},
		IdentityKey: "movie:atomic", Title: "Atomic", Status: "discovered", Data: []byte("{}"),
	})
	require.NoError(t, err)
	metadataArtifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "fetch_result", SchemaVersion: 1, Data: []byte("{}"), PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	applyArtifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "apply_result", SchemaVersion: 1, Data: []byte("{}"),
		PipelineGeneration: entity.PipelineGeneration, SourceArtifactID: pgtype.Int8{Int64: metadataArtifact.ID, Valid: true},
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE scanner_entities
		SET status = 'applying', metadata_artifact_id = $2, apply_artifact_id = $3
		WHERE id = $1`, entity.ID, metadataArtifact.ID, applyArtifact.ID)
	require.NoError(t, err)

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	const mediaItemID int64 = 880002
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = 'ratings_fetch' AND (args->>'media_item_id')::bigint = $1`, mediaItemID)
	})
	result := scanner.Result{MovieApply: []scanner.MovieApplyResult{{Action: "create", MediaItemID: mediaItemID}}}

	// Simulate a failure immediately before commit. Both the queued work and
	// terminal status disappear, leaving the checkpoint retryable.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = rc.InsertTx(ctx, tx, RatingsFetchArgs{MediaItemID: mediaItemID, LibraryID: lib.ID}, nil)
	require.NoError(t, err)
	finalized, err := scanner.FinalizeScannerApplyEntityTx(ctx, tx, entity.ID, metadataArtifact.ID, applyArtifact.ID, result)
	require.NoError(t, err)
	require.True(t, finalized)
	require.NoError(t, tx.Rollback(ctx))

	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "applying", entity.Status)
	var jobs int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'ratings_fetch' AND (args->>'media_item_id')::bigint = $1`, mediaItemID).Scan(&jobs))
	require.Zero(t, jobs)

	// The retry commits the same two operations together.
	tx, err = pool.Begin(ctx)
	require.NoError(t, err)
	_, err = rc.InsertTx(ctx, tx, RatingsFetchArgs{MediaItemID: mediaItemID, LibraryID: lib.ID}, nil)
	require.NoError(t, err)
	finalized, err = scanner.FinalizeScannerApplyEntityTx(ctx, tx, entity.ID, metadataArtifact.ID, applyArtifact.ID, result)
	require.NoError(t, err)
	require.True(t, finalized)
	require.NoError(t, tx.Commit(ctx))

	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", entity.Status)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'ratings_fetch' AND (args->>'media_item_id')::bigint = $1`, mediaItemID).Scan(&jobs))
	require.Equal(t, 1, jobs)
}

func TestApplyRichMetadataRejectsChangedArtifactSourcesBeforeStore(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	root := t.TempDir()
	scope := filepath.Join(root, "Movie (2026)")
	require.NoError(t, os.MkdirAll(scope, 0o755))
	path := filepath.Join(scope, "Movie (2026).mkv")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "stale-rich-metadata", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	var entityID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entities (
			library_id, media_type, scope_key, scope_paths, identity_key, title, status, pipeline_generation
		) VALUES ($1, $2, 'movie-scope', $3, 'movie-key', 'Movie', 'applied', 1)
		RETURNING id
	`, lib.ID, lib.MediaType, []string{scope}).Scan(&entityID))

	data, err := json.Marshal(map[string]any{
		"schema_version":    1,
		"pipeline_revision": 2,
		"inventory": map[string]any{"roots": []any{map[string]any{
			"root": root,
			"files": []scanner.InventoryFile{{
				Root: root, Path: path, RelPath: "Movie (2026)/Movie (2026).mkv",
				Class: scanner.ClassPrimaryMedia, Size: info.Size(), MTime: info.ModTime(),
			}},
		}}},
		"result": scanner.Result{MovieMetadata: []scanner.MovieFetchPreview{{
			Key: "movie-key", ProviderID: "movie-provider", Detail: &metadata.MediaDetail{Title: "Movie"},
		}}},
	})
	require.NoError(t, err)
	var artifactID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entity_artifacts (entity_id, stage, schema_version, data, pipeline_generation)
		VALUES ($1, 'fetch_result', 1, $2, 1)
		RETURNING id
	`, entityID, data).Scan(&artifactID))
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET metadata_artifact_id = $2 WHERE id = $1`, entityID, artifactID)
	require.NoError(t, err)

	// Change the exact source captured by the fetch artifact before the rich
	// side-data worker resumes it.
	require.NoError(t, os.WriteFile(path, []byte("replacement bytes"), 0o600))

	insertClient, err := NewInsertClient(pool)
	require.NoError(t, err)
	workCtx := rivertest.WorkContext(ctx, insertClient)
	matcher := &richMetadataCallCounter{}
	worker := ApplyRichMetadataWorker{DB: pool, Matcher: matcher, Progress: &TaskProgressBroadcaster{}}
	err = worker.Work(workCtx, &river.Job[ApplyRichMetadataArgs]{JobRow: &rivertype.JobRow{ID: 99101}, Args: ApplyRichMetadataArgs{
		LibraryID: lib.ID, MediaItemID: 99102, ScannerEntityID: entityID,
		MetadataArtifactID: artifactID, MediaKind: string(metadata.KindMovie), Key: "movie-key",
	}})
	require.NoError(t, err)
	require.Zero(t, matcher.calls, "stale artifact reached StoreRichMetadata")

	var queuedScopes []string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT ARRAY(SELECT jsonb_array_elements_text(args->'scope_paths'))
		FROM river_job
		WHERE kind = 'process_scan' AND (args->>'library_id')::bigint = $1
		ORDER BY id DESC LIMIT 1
	`, lib.ID).Scan(&queuedScopes))
	require.Equal(t, []string{scope}, queuedScopes)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = 'process_scan' AND (args->>'library_id')::bigint = $1`, lib.ID)
	})
}

func TestApplyRichMetadataRematchBetweenLoadAndStoreCannotCommit(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	root := t.TempDir()
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "rich-metadata-rematch-race", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	var entityID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entities (
			library_id, media_type, scope_key, scope_paths, identity_key, title, status, pipeline_generation
		) VALUES ($1, $2, 'movie-race-scope', $3, 'movie-race-key', 'Movie', 'applied', 1)
		RETURNING id
	`, lib.ID, lib.MediaType, []string{root}).Scan(&entityID))

	// An empty source root still has a real source-set digest (SHA-256 of no
	// entries), keeping this test focused on generation lineage rather than
	// filesystem invalidation.
	data, err := json.Marshal(map[string]any{
		"schema_version":    1,
		"pipeline_revision": 3,
		"source_set": map[string]any{"roots": []any{map[string]any{
			"root": root, "rel_starts": []string{"."}, "count": 0,
			"sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		}}},
		"inventory": map[string]any{"roots": []any{map[string]any{"root": root}}},
		"result": scanner.Result{MovieMetadata: []scanner.MovieFetchPreview{{
			Key: "movie-race-key", ProviderID: "movie-provider", Detail: &metadata.MediaDetail{Title: "Old Movie"},
		}}},
	})
	require.NoError(t, err)
	var artifactID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entity_artifacts (entity_id, stage, schema_version, data, pipeline_generation)
		VALUES ($1, 'fetch_result', 1, $2, 1)
		RETURNING id
	`, entityID, data).Scan(&artifactID))
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET metadata_artifact_id = $2 WHERE id = $1`, entityID, artifactID)
	require.NoError(t, err)

	matcher := &richMetadataCallCounter{}
	worker := ApplyRichMetadataWorker{
		DB: pool, Matcher: matcher, Progress: &TaskProgressBroadcaster{},
		BeforeStoreTransaction: func() error {
			// This is the exact dangerous interleaving: the worker already loaded
			// old detail, then a manual decision/rematch supersedes its generation.
			_, updateErr := pool.Exec(ctx, `
				UPDATE scanner_entities
				SET pipeline_generation = pipeline_generation + 1,
				    metadata_artifact_id = NULL,
				    status = 'discovered'
				WHERE id = $1
			`, entityID)
			return updateErr
		},
	}
	err = worker.Work(ctx, &river.Job[ApplyRichMetadataArgs]{JobRow: &rivertype.JobRow{ID: 99103}, Args: ApplyRichMetadataArgs{
		LibraryID: lib.ID, MediaItemID: 99104, ScannerEntityID: entityID,
		MetadataArtifactID: artifactID, MediaKind: string(metadata.KindMovie), Key: "movie-race-key",
	}})
	require.NoError(t, err)
	require.Zero(t, matcher.calls, "superseded rich metadata reached transactional persistence")
}

// The DB round-trips mtimes at Postgres's µs precision while a fresh
// os.Stat carries nanoseconds — the comparison must truncate both sides or
// every file with sub-µs mtime residue reads as changed on every scan,
// silently degrading incremental scans into full reprocesses.
func TestLibraryFileChangedTruncatesMtimeToMicroseconds(t *testing.T) {
	statMtime := time.Date(2026, 7, 10, 4, 0, 0, 123456789, time.UTC) // ns residue
	dbMtime := statMtime.Truncate(time.Microsecond)                   // what PG returns

	row := sqlc.ListLibraryFilesForScanRow{
		Size:  42,
		Mtime: pgtype.Timestamptz{Time: dbMtime, Valid: true},
	}
	file := scanner.InventoryFile{Size: 42, MTime: statMtime}
	require.False(t, libraryFileChanged(row, file), "µs-truncated equal mtimes must read as unchanged")

	file.MTime = statMtime.Add(2 * time.Second)
	require.True(t, libraryFileChanged(row, file), "a real mtime change must still be detected")

	file.MTime = statMtime
	file.Size = 43
	require.True(t, libraryFileChanged(row, file), "a size change must still be detected")
}

func TestCountFetchedResultItemsRequiresUsableDetail(t *testing.T) {
	require.Zero(t, countFetchedResultItems(nil, []scanner.BookFetchPreview{{
		ProviderID: "heya:book:accepted-but-failed", Error: "upstream status 500",
	}}, nil, nil), "a provider ID without usable detail must not enqueue apply")
	require.Zero(t, countFetchedResultItems(nil, []scanner.BookFetchPreview{{
		ProviderID: "heya:book:empty-detail",
	}}, nil, nil))
	require.Equal(t, 1, countFetchedResultItems(nil, []scanner.BookFetchPreview{{
		ProviderID: "heya:book:usable", Detail: &metadata.MediaDetail{Title: "Book"},
	}}, nil, nil))
}

func TestPendingLibraryFileDoesNotSupersedeLivePipeline(t *testing.T) {
	mtime := time.Date(2026, 7, 10, 4, 0, 0, 123456000, time.UTC)
	row := sqlc.ListLibraryFilesForScanRow{
		Status: sqlc.FileStatusPending,
		Size:   42,
		Mtime:  pgtype.Timestamptz{Time: mtime, Valid: true},
	}
	file := scanner.InventoryFile{Size: 42, MTime: mtime}
	scope := "/music/Ado"
	live := liveScannerPipelineScopes{scopes: []string{scope}}

	require.False(t, libraryFileNeedsScan(row, file, scope, live), "unchanged pending source must wait for its live generation")
	require.True(t, libraryFileNeedsScan(row, file, scope, liveScannerPipelineScopes{}), "orphaned pending source must self-heal")

	file.Size++
	require.True(t, libraryFileNeedsScan(row, file, scope, live), "real byte changes must supersede even a live generation")
}

func TestLoadLiveScannerPipelineScopesIncludesParkedContinuation(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "live-scanner-scope-test", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/music"}, ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	const scope = "/music/Ado"
	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope-test", ScopePaths: []string{scope},
		IdentityKey: "artist:ado", Title: "Ado", Status: "discovered", Data: []byte("{}"),
	})
	require.NoError(t, err)
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: "analysis_result", SchemaVersion: 1, Data: []byte("{}"),
		PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO scanner_metadata_continuations(kind,library_id,scanner_entity_id,artifact_id,args,next_attempt_at) VALUES('search_metadata',$1,$2,$3,'{}'::jsonb,now()+interval '1 hour')`, lib.ID, entity.ID, artifact.ID)
	require.NoError(t, err)

	live, err := loadLiveScannerPipelineScopes(ctx, pool, lib.ID)
	require.NoError(t, err)
	require.True(t, live.overlaps(scope))
	require.False(t, live.overlaps("/music/Someone Else"))
}

func TestTimestamptzChangedTruncatesToMicroseconds(t *testing.T) {
	statMtime := time.Date(2026, 7, 10, 4, 0, 0, 999999999, time.UTC)
	dbMtime := statMtime.Truncate(time.Microsecond)

	a := pgtype.Timestamptz{Time: dbMtime, Valid: true}
	b := pgtype.Timestamptz{Time: statMtime, Valid: true}
	require.False(t, timestamptzChanged(a, b), "µs-truncated equal timestamps must read as unchanged")

	b.Time = statMtime.Add(time.Millisecond)
	require.True(t, timestamptzChanged(a, b), "a >1µs difference must still be detected")

	require.True(t, timestamptzChanged(a, pgtype.Timestamptz{}), "validity mismatch must read as changed")
}

func TestMatchMovedFilesPairsBySizePlusBasenameOrMtime(t *testing.T) {
	mtime := time.Date(2026, 7, 10, 4, 0, 0, 123456789, time.UTC)
	rows := []sqlc.ListLibraryFilesForScanRow{
		{ID: 1, Path: "/media/Movies/Old Name (1999)/movie.mkv", Size: 100, Mtime: pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true}},
		{ID: 2, Path: "/media/Movies/Kept (2001)/kept.mkv", Size: 100, Mtime: pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true}},
		{ID: 3, Path: "/media/Movies/Renamed (2002)/before.mkv", Size: 300, Mtime: pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true}},
	}
	seen := map[string]bool{"/media/Movies/Kept (2001)/kept.mkv": true} // still on disk — never a candidate

	moves := matchMovedFiles(rows, seen, []scanner.InventoryFile{
		// moved across dirs: same size + same basename, mtime irrelevant
		{Path: "/media/Movies/New Name (1999)/movie.mkv", Size: 100, MTime: mtime.Add(time.Hour)},
		// renamed in place: same size + same µs-mtime, basename differs
		{Path: "/media/Movies/Renamed (2002)/after.mkv", Size: 300, MTime: mtime},
		// same size as row 1 but different basename AND mtime: no claim
		{Path: "/media/Movies/Impostor (2020)/impostor.mkv", Size: 100, MTime: mtime.Add(48 * time.Hour)},
	})

	require.Len(t, moves, 2)
	byID := map[int64]string{}
	for _, m := range moves {
		byID[m.Row.ID] = m.File.Path
	}
	require.Equal(t, "/media/Movies/New Name (1999)/movie.mkv", byID[1])
	require.Equal(t, "/media/Movies/Renamed (2002)/after.mkv", byID[3])
}

func TestMatchMovedFilesNeverClaimsBySizeAlone(t *testing.T) {
	rows := []sqlc.ListLibraryFilesForScanRow{
		{ID: 1, Path: "/media/Movies/Gone (1999)/gone.mkv", Size: 100},
	}
	moves := matchMovedFiles(rows, map[string]bool{}, []scanner.InventoryFile{
		{Path: "/media/Movies/Fresh (2024)/fresh.mkv", Size: 100, MTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	require.Empty(t, moves, "size alone must not transfer a row's identity")
}

func TestMatchMovedFilesSkipsStaleSoftDeletes(t *testing.T) {
	old := pgtype.Timestamptz{Time: time.Now().Add(-8 * 24 * time.Hour), Valid: true}
	recent := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
	rows := []sqlc.ListLibraryFilesForScanRow{
		{ID: 1, Path: "/media/Movies/Stale (1999)/movie.mkv", Size: 100, DeletedAt: old},
		{ID: 2, Path: "/media/Movies/Recent (2001)/movie.mkv", Size: 100, DeletedAt: recent},
	}
	moves := matchMovedFiles(rows, map[string]bool{}, []scanner.InventoryFile{
		{Path: "/media/Movies/Moved (2001)/movie.mkv", Size: 100},
	})
	require.Len(t, moves, 1)
	require.Equal(t, int64(2), moves[0].Row.ID, "stale soft-deletes are out of the 7-day window; the recent one wins")
}

func TestRelocateMovedFilesKeepsRowIDAndEscapesSoftDelete(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "kickoff-move-detection-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	mtime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	orig, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID:   lib.ID,
		Path:        "/media/movies/Old Title (1999)/Old Title.mkv",
		Size:        100,
		Mtime:       pgtype.Timestamptz{Time: mtime, Valid: true},
		ParseResult: []byte("{}"),
		Status:      sqlc.FileStatusMatched,
	})
	require.NoError(t, err)

	rows, err := q.ListLibraryFilesForScan(ctx, lib.ID)
	require.NoError(t, err)

	w := &KickoffLibraryScanWorker{DB: pool}
	seen := map[string]bool{}
	var scopes []string
	moved := w.relocateMovedFiles(ctx, q, lib, rows, seen, []scanner.InventoryFile{
		{Path: "/media/movies/New Title (1999)/Old Title.mkv", RelPath: "New Title (1999)/Old Title.mkv", Size: 100, MTime: mtime},
	}, func(scope string) { scopes = append(scopes, scope) })

	require.Equal(t, 1, moved)
	require.True(t, seen["/media/movies/Old Title (1999)/Old Title.mkv"], "old path must escape the soft-delete pass")
	require.Contains(t, scopes, "/media/movies/Old Title (1999)", "old owner scope re-enters the pipeline")

	row, err := q.GetLibraryFileByID(ctx, orig.ID)
	require.NoError(t, err)
	require.Equal(t, "/media/movies/New Title (1999)/Old Title.mkv", row.Path, "row keeps its id under the new path")
	require.False(t, row.DeletedAt.Valid)
}

// Compaction deletes ALL of an entity's artifacts, so the guard must key on
// the entity (not a single metadata_artifact_id, which let a newer apply
// cycle's compaction delete an older cycle's still-referenced artifact) and
// cover every pipeline kind that could still produce or consume a rich job —
// a live fetch/apply cycle will enqueue one we haven't seen yet.
func TestActiveScannerJobsForEntityGuardsByEntity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	const entityWithJob, otherEntity int64 = 991001, 991002

	insertJob := func(kind string) int64 {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO river_job (kind, queue, args, max_attempts, state)
			VALUES ($1, $1, $2, 5, 'available')
			RETURNING id`, kind, []byte(`{"scanner_entity_id": 991001}`)).Scan(&id)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, id) })
		return id
	}

	// Each pipeline kind that can still lead to a rich job must block compaction,
	// including a mid-flight apply cycle that hasn't enqueued its rich job yet.
	for _, kind := range []string{"search_metadata", "fetch_metadata", "apply_metadata", "apply_rich_metadata"} {
		jobID := insertJob(kind)

		busy, err := activeScannerJobsForEntity(ctx, pool, entityWithJob, 0)
		require.NoError(t, err)
		require.True(t, busy, "a pending %s for the entity must block compaction", kind)

		busy, err = activeScannerJobsForEntity(ctx, pool, entityWithJob, jobID)
		require.NoError(t, err)
		require.False(t, busy, "the compacting job excludes itself (%s)", kind)

		busy, err = activeScannerJobsForEntity(ctx, pool, otherEntity, 0)
		require.NoError(t, err)
		require.False(t, busy, "an unrelated entity is not blocked (%s)", kind)

		_, err = pool.Exec(ctx, `DELETE FROM river_job WHERE id = $1`, jobID)
		require.NoError(t, err)
	}
}

func TestOversizedScannerArtifactCancelsWorkerRetry(t *testing.T) {
	err := &scanner.ArtifactTooLargeError{Kind: "search_result", Size: 17, Limit: 16}
	got := scannerWorkerError(err)

	require.ErrorIs(t, got, river.JobCancel(errors.New("permanent")))
	require.ErrorIs(t, got, err)
}

func TestKickoffLibraryScanSupportsScannerDomains(t *testing.T) {
	for _, mt := range []sqlc.MediaType{sqlc.MediaTypeMovie, sqlc.MediaTypeTv, sqlc.MediaTypeAnime, sqlc.MediaTypeMusic, sqlc.MediaTypeBook} {
		require.True(t, supportsScanner(mt), "%s should use scanner", mt)
	}

	for _, mt := range []sqlc.MediaType{sqlc.MediaTypeComic, sqlc.MediaTypePodcast, sqlc.MediaTypeRadio} {
		require.False(t, supportsScanner(mt), "%s should not fall back to the legacy scanner", mt)
	}
}

func TestScannerPipelineQueuesArePartitionedByMediaType(t *testing.T) {
	tests := []struct {
		name      string
		mediaType sqlc.MediaType
		wantQueue string
	}{
		{name: "movies", mediaType: sqlc.MediaTypeMovie, wantQueue: "process_scan_movie"},
		{name: "tv", mediaType: sqlc.MediaTypeTv, wantQueue: "process_scan_tv"},
		{name: "anime", mediaType: sqlc.MediaTypeAnime, wantQueue: "process_scan_anime"},
		{name: "music", mediaType: sqlc.MediaTypeMusic, wantQueue: "process_scan_music"},
		{name: "books", mediaType: sqlc.MediaTypeBook, wantQueue: "process_scan_book"},
		{name: "unknown fallback", mediaType: sqlc.MediaType("future"), wantQueue: "process_scan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantQueue, ProcessLibraryScanArgs{MediaType: tt.mediaType}.InsertOpts().Queue)
		})
	}

	require.Equal(t, "kickoff_library_scan_anime", KickoffLibraryScanArgs{MediaType: sqlc.MediaTypeAnime}.InsertOpts().Queue)
	require.Equal(t, "search_metadata_music", SearchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic}.InsertOpts().Queue)
	require.Equal(t, "search_metadata_poll_music", SearchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic, Poll: true}.InsertOpts().Queue)
	require.Equal(t, "fetch_metadata_music", FetchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic}.InsertOpts().Queue)
	require.Equal(t, "fetch_metadata_poll_music", FetchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic, Poll: true}.InsertOpts().Queue)
	require.Equal(t, "apply_metadata_tv", ApplyLibraryScanArgs{MediaType: sqlc.MediaTypeTv}.InsertOpts().Queue)
}

func TestRemoteMetadataPollContinuationsAreParkedOutsideRiver(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "parked-polls",
		MediaType:    sqlc.MediaTypeAnime,
		Paths:        []string{"/media/parked-polls"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	makeArtifact := func(identity, stage string) (int64, int64) {
		var entityID, artifactID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO scanner_entities (library_id, media_type, identity_key, title)
			VALUES ($1, $2, $3, $3) RETURNING id
		`, lib.ID, lib.MediaType, identity).Scan(&entityID))
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO scanner_entity_artifacts (entity_id, stage) VALUES ($1, $2) RETURNING id
		`, entityID, stage).Scan(&artifactID))
		return entityID, artifactID
	}
	searchEntityID, analysisArtifactID := makeArtifact("search", "analysis")
	fetchEntityID, searchArtifactID := makeArtifact("fetch", "search")

	searchArgs := SearchLibraryMetadataArgs{LibraryID: lib.ID, MediaType: lib.MediaType, ScannerEntityID: searchEntityID, AnalysisArtifactID: analysisArtifactID, Poll: true}
	fetchArgs := FetchLibraryMetadataArgs{LibraryID: lib.ID, MediaType: lib.MediaType, ScannerEntityID: fetchEntityID, SearchArtifactID: searchArtifactID, Poll: true}
	require.NoError(t, parkMetadataContinuation(ctx, pool, searchArgs.Kind(), lib.ID, searchEntityID, analysisArtifactID, searchArgs, PriorityScan, "manual", time.Minute, metadataContinuationWorkflow{}))
	require.NoError(t, parkMetadataContinuation(ctx, pool, fetchArgs.Kind(), lib.ID, fetchEntityID, searchArtifactID, fetchArgs, PriorityScan, "", time.Minute, metadataContinuationWorkflow{}))

	rows, err := pool.Query(ctx, `
		SELECT kind, args->>'poll', source
		FROM scanner_metadata_continuations
		WHERE library_id = $1
		ORDER BY kind`, lib.ID)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var kind, poll, source string
		require.NoError(t, rows.Scan(&kind, &poll, &source))
		got[kind] = [2]string{poll, source}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, [2]string{"true", "manual"}, got["search_metadata"])
	require.Equal(t, [2]string{"true", ""}, got["fetch_metadata"])
}

func TestMetadataContinuationSweepAdoptsLegacyRiverPolls(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "legacy-polls",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/legacy-polls"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	var entityID, artifactID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entities (library_id, media_type, identity_key, title)
		VALUES ($1, $2, 'legacy-poll', 'legacy-poll') RETURNING id
	`, lib.ID, lib.MediaType).Scan(&entityID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entity_artifacts (entity_id, stage) VALUES ($1, 'analysis') RETURNING id
	`, entityID).Scan(&artifactID))

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	legacyArgs := SearchLibraryMetadataArgs{
		LibraryID:          lib.ID,
		MediaType:          lib.MediaType,
		ScannerEntityID:    entityID,
		AnalysisArtifactID: artifactID,
		Poll:               true,
	}
	legacyOpts := legacyArgs.InsertOpts()
	legacyOpts.ScheduledAt = time.Now().Add(time.Hour)
	inserted, err := rc.Insert(ctx, legacyArgs, &legacyOpts)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, inserted.Job.ID) })

	sweeper := MetadataContinuationSweepWorker{DB: pool}
	adopted, err := sweeper.adoptLegacyPollJobs(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, adopted, 1)

	var riverRows, continuations int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE id = $1`, inserted.Job.ID).Scan(&riverRows))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM scanner_metadata_continuations
		WHERE kind = 'search_metadata' AND scanner_entity_id = $1 AND artifact_id = $2
	`, entityID, artifactID).Scan(&continuations))
	require.Zero(t, riverRows)
	require.Equal(t, 1, continuations)
}

func TestRenameLegacyScannerJobsRoutesActiveBacklogByMediaType(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)

	createLibrary := func(name string, mediaType sqlc.MediaType) sqlc.Library {
		lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
			Name:         name,
			MediaType:    mediaType,
			Paths:        []string{"/media/" + name},
			ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
			CreatedBy:    userID,
			Settings:     []byte("{}"),
		})
		require.NoError(t, err)
		t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
		return lib
	}

	music := createLibrary("typed-queue-music", sqlc.MediaTypeMusic)
	anime := createLibrary("typed-queue-anime", sqlc.MediaTypeAnime)
	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)

	musicJob, err := rc.Insert(ctx, ProcessLibraryScanArgs{
		LibraryID:  music.ID,
		ScopePaths: []string{"/media/typed-queue-music/Artist"},
	}, nil)
	require.NoError(t, err)
	animeJob, err := rc.Insert(ctx, ProcessLibraryScanArgs{
		LibraryID:  anime.ID,
		ScopePaths: []string{"/media/typed-queue-anime/Series"},
	}, nil)
	require.NoError(t, err)
	musicPollJob, err := rc.Insert(ctx, FetchLibraryMetadataArgs{
		LibraryID:        music.ID,
		ScannerEntityID:  991,
		SearchArtifactID: 992,
		Poll:             true,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = ANY($1::bigint[])`, []int64{musicJob.Job.ID, animeJob.Job.ID, musicPollJob.Job.ID})
	})

	queueFor := func(jobID int64) string {
		var queue string
		require.NoError(t, pool.QueryRow(ctx, `SELECT queue FROM river_job WHERE id = $1`, jobID).Scan(&queue))
		return queue
	}
	require.Equal(t, "process_scan", queueFor(musicJob.Job.ID))
	require.Equal(t, "process_scan", queueFor(animeJob.Job.ID))
	require.Equal(t, "fetch_metadata_poll", queueFor(musicPollJob.Job.ID))

	require.NoError(t, renameLegacyScannerJobs(ctx, pool))
	require.Equal(t, "process_scan_music", queueFor(musicJob.Job.ID))
	require.Equal(t, "process_scan_anime", queueFor(animeJob.Job.ID))
	require.Equal(t, "fetch_metadata_poll_music", queueFor(musicPollJob.Job.ID))
}

func TestScannerInventoryPostApplyPaths(t *testing.T) {
	inv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
		Root: "/media",
		Files: []scanner.InventoryFile{
			{Path: "/media/Movie (2021)/Movie (2021).mkv", Class: scanner.ClassPrimaryMedia},
			{Path: "/media/Movie (2021)/trailers/trailer.mp4", Class: scanner.ClassExtraMedia},
			{Path: "/media/Movie (2021)/subtitles/en.srt", Class: scanner.ClassSubtitle},
			{Path: "/media/Movie (2021)/poster.jpg", Class: scanner.ClassArtwork},
			{Path: "/media/Music/Album/01 Track.flac", Class: scanner.ClassPrimaryMedia},
			{Path: "/media/Music/Album/01 Track.flac", Class: scanner.ClassPrimaryMedia},
		},
	}}}

	require.Equal(t, []string{
		"/media/Movie (2021)/Movie (2021).mkv",
		"/media/Movie (2021)/trailers/trailer.mp4",
		"/media/Music/Album/01 Track.flac",
	}, scannerInventoryPostApplyPaths(inv))
}

func TestPostApplySonicDeduplicatesTracksWithMultipleFiles(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "post-apply-sonic-dedupe",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/media/post-apply-sonic-dedupe"},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: sqlc.MediaTypeMusic,
		Title: "Dedupe Artist", SortTitle: "Dedupe Artist", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	artist, err := q.CreateArtist(ctx, sqlc.CreateArtistParams{MediaItemID: item.ID, Name: item.Title})
	require.NoError(t, err)
	album, err := q.CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ArtistID: artist.ID, Title: "Dedupe Album", Year: "2026", Genres: []string{}, Tags: []string{},
	})
	require.NoError(t, err)

	paths := []string{
		"/media/post-apply-sonic-dedupe/01 Track.flac",
		"/media/post-apply-sonic-dedupe/01 Track.mp3",
	}
	track, err := q.CreateTrack(ctx, sqlc.CreateTrackParams{
		AlbumID: album.ID, DiscNumber: 1, TrackNumber: 1, Title: "Track",
	})
	require.NoError(t, err)
	fileIDs := make([]int64, 0, len(paths))
	trackFileIDs := make([]int64, 0, len(paths))
	for _, path := range paths {
		file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: lib.ID, Path: path, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
		})
		require.NoError(t, err)
		trackFile, err := q.UpsertTrackFile(ctx, sqlc.UpsertTrackFileParams{
			TrackID: track.ID, LibraryFileID: file.ID,
		})
		require.NoError(t, err)
		fileIDs = append(fileIDs, file.ID)
		trackFileIDs = append(trackFileIDs, trackFile.ID)
	}
	_, err = pool.Exec(ctx, `
		UPDATE library_files
		SET media_info = '{"streams":[{"codec_type":"audio"}]}'::jsonb
		WHERE id = ANY($1::bigint[])`, fileIDs)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		UPDATE track_files
		SET integrated_lufs = -14, boundaries_analyzed_at = now()
		WHERE id = ANY($1::bigint[])`, trackFileIDs)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO library_file_fingerprints (
			library_file_id, algorithm, fingerprint, fingerprint_duration_secs,
			source_duration_secs, source_size, source_mtime
		)
		SELECT id, 1, 'test-fingerprint-' || id, 1, 1, size, mtime
		FROM library_files WHERE id = ANY($1::bigint[])`, fileIDs)
	require.NoError(t, err)

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)
	taskID := "post-apply-sonic-dedupe"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE args->>'scheduled_task_id' = $1`, taskID)
	})
	result := scanner.Result{Inventory: scanner.Inventory{Roots: []scanner.InventoryRoot{{
		Root: "/media/post-apply-sonic-dedupe",
		Files: []scanner.InventoryFile{
			{Path: paths[0], Class: scanner.ClassPrimaryMedia},
			{Path: paths[1], Class: scanner.ClassPrimaryMedia},
		},
	}}}}
	worker := &ApplyLibraryScanWorker{SonicEnabled: func(context.Context) bool { return true }}
	fanout := worker.enqueuePostApplyWork(ctx, q, rc, lib, result, taskID, "")

	require.Equal(t, 2, fanout.Files)
	require.Equal(t, 1, fanout.Sonic)
	require.Equal(t, 1, fanout.Skipped)
	require.Zero(t, fanout.Failed)

	var jobs int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM river_job
		WHERE kind = 'analyze_track_facets'
		  AND (args->>'track_id')::bigint = $1
		  AND args->>'scheduled_task_id' = $2`, track.ID, taskID).Scan(&jobs))
	require.Equal(t, 1, jobs)
}

func TestCompactScannerScopesDropsChildren(t *testing.T) {
	require.Equal(t, []string{
		"/library/Movie (2021)",
		"/library/Other (2022)",
	}, compactScannerScopes([]string{
		"/library/Movie (2021)",
		"/library/Movie (2021)/trailers",
		"/library/Movie (2021)/featurettes",
		"/library/Other (2022)",
	}))
}

func TestScannerScopeForPathUsesOwningMediaDirectory(t *testing.T) {
	require.Equal(t,
		"/library/Show (2024)",
		ScannerScopeForPath(sqlc.MediaTypeTv, "/library/Show (2024)/Season 01/Show.S01E01.mkv"),
	)
	require.Equal(t,
		"/library/Show (2024)",
		ScannerScopeForPath(sqlc.MediaTypeAnime, "/library/Show (2024)/Season 01/featurettes/Behind The Scenes.mkv"),
	)
	require.Equal(t,
		"/library/Movie (2024)",
		ScannerScopeForPath(sqlc.MediaTypeMovie, "/library/Movie (2024)/trailers/trailer.mkv"),
	)
	require.Equal(t,
		"/library/Music/Samples",
		ScannerScopeForPath(sqlc.MediaTypeMusic, "/library/Music/Samples/01 Track.flac"),
	)
}

func TestScannerScopeForInventoryFileKeepsTopLevelMediaFileScoped(t *testing.T) {
	file := scanner.InventoryFile{
		Path:    "/library/Loose.Movie.2024.1080p.WEB-DL.mkv",
		RelPath: "Loose.Movie.2024.1080p.WEB-DL.mkv",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, file.Path, scannerScopeForInventoryFile(sqlc.MediaTypeMovie, file))

	nested := scanner.InventoryFile{
		Path:    "/library/Movie (2024)/Movie.2024.mkv",
		RelPath: "Movie (2024)/Movie.2024.mkv",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, "/library/Movie (2024)", scannerScopeForInventoryFile(sqlc.MediaTypeMovie, nested))
}

func TestScannerScopeForInventoryFileUsesMusicArtistScope(t *testing.T) {
	albumTrack := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/ano/2022 - Chu,Tayousei./01 - Chu,Tayousei.flac",
		RelPath: "ano/2022 - Chu,Tayousei./01 - Chu,Tayousei.flac",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, "/library/Music/ano", scannerScopeForInventoryFile(sqlc.MediaTypeMusic, albumTrack))

	artistTrack := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/ano/01 - Loose.flac",
		RelPath: "ano/01 - Loose.flac",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, "/library/Music/ano", scannerScopeForInventoryFile(sqlc.MediaTypeMusic, artistTrack))

	looseTrack := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/loose.mp3",
		RelPath: "loose.mp3",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, looseTrack.Path, scannerScopeForInventoryFile(sqlc.MediaTypeMusic, looseTrack))

	albumNFO := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/ano/2022 - Chu,Tayousei./album.nfo",
		RelPath: "ano/2022 - Chu,Tayousei./album.nfo",
		Class:   scanner.ClassNFO,
	}
	require.Equal(t, "/library/Music/ano", scannerScopeForInventoryFile(sqlc.MediaTypeMusic, albumNFO))
}

func TestScannerScopeForLibraryPathUsesMusicArtistScope(t *testing.T) {
	lib := sqlc.Library{
		MediaType: sqlc.MediaTypeMusic,
		Paths:     []string{"/library/Music"},
	}

	require.Equal(t,
		"/library/Music/Daft Punk",
		ScannerScopeForLibraryPath(lib, "/library/Music/Daft Punk/1997 - Homework/01 - Daftendirekt.flac"),
	)
	require.Equal(t,
		"/library/Music/Daft Punk",
		ScannerScopeForLibraryPath(lib, "/library/Music/Daft Punk/1997 - Homework/album.nfo"),
	)
	require.Equal(t,
		"/library/Music/Daft Punk",
		ScannerScopeForLibraryPath(lib, "/library/Music/Daft Punk"),
	)
}

func TestScannerScopeForLibraryDirectoryKeepsNFOOwnerScope(t *testing.T) {
	tests := []struct {
		name string
		lib  sqlc.Library
		dir  string
		want string
	}{
		{
			name: "TV show",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeTv, Paths: []string{"/storage/TV/Foreign"}},
			dir:  "/storage/TV/Foreign/Some Show",
			want: "/storage/TV/Foreign/Some Show",
		},
		{
			name: "TV season promotes to show",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeTv, Paths: []string{"/storage/TV/Foreign"}},
			dir:  "/storage/TV/Foreign/Some Show/Season 01",
			want: "/storage/TV/Foreign/Some Show",
		},
		{
			name: "movie",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeMovie, Paths: []string{"/storage/Movies"}},
			dir:  "/storage/Movies/Dune (2021)",
			want: "/storage/Movies/Dune (2021)",
		},
		{
			name: "mounted TV show",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeTv, Paths: []string{"/mnt/nas/media/TV"}},
			dir:  "/mnt/nas/media/TV/Some Show",
			want: "/mnt/nas/media/TV/Some Show",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ScannerScopeForLibraryDirectory(tt.lib, tt.dir))
		})
	}
}

func TestProcessLibraryScanFanoutSplitsFullAndRootScopesIntoOwners(t *testing.T) {
	tests := []struct {
		name      string
		lib       sqlc.Library
		inventory scanner.Inventory
		want      []string
	}{
		{
			name: "local TV",
			lib: sqlc.Library{
				ID:        3,
				MediaType: sqlc.MediaTypeTv,
				Paths:     []string{"/storage/TV/Foreign"},
			},
			inventory: scanner.Inventory{Roots: []scanner.InventoryRoot{{
				Root: "/storage/TV/Foreign",
				Files: []scanner.InventoryFile{
					{Root: "/storage/TV/Foreign", Path: "/storage/TV/Foreign/Alpha/Season 01/Alpha.S01E01.mkv", RelPath: "Alpha/Season 01/Alpha.S01E01.mkv", Class: scanner.ClassPrimaryMedia},
					{Root: "/storage/TV/Foreign", Path: "/storage/TV/Foreign/Beta/Season 02/Beta.S02E01.mkv", RelPath: "Beta/Season 02/Beta.S02E01.mkv", Class: scanner.ClassPrimaryMedia},
				},
			}}},
			want: []string{"/storage/TV/Foreign/Alpha", "/storage/TV/Foreign/Beta"},
		},
		{
			name: "mounted network movies",
			lib: sqlc.Library{
				ID:        4,
				MediaType: sqlc.MediaTypeMovie,
				Paths:     []string{"/mnt/nas/media/Movies"},
			},
			inventory: scanner.Inventory{Roots: []scanner.InventoryRoot{{
				Root: "/mnt/nas/media/Movies",
				Files: []scanner.InventoryFile{
					{Root: "/mnt/nas/media/Movies", Path: "/mnt/nas/media/Movies/Alien (1979)/Alien.mkv", RelPath: "Alien (1979)/Alien.mkv", Class: scanner.ClassPrimaryMedia},
					{Root: "/mnt/nas/media/Movies", Path: "/mnt/nas/media/Movies/Dune (2021)/Dune.mkv", RelPath: "Dune (2021)/Dune.mkv", Class: scanner.ClassPrimaryMedia},
				},
			}}},
			want: []string{"/mnt/nas/media/Movies/Alien (1979)", "/mnt/nas/media/Movies/Dune (2021)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := ProcessLibraryScanArgs{LibraryID: tt.lib.ID, Force: true}
			for _, requested := range [][]string{nil, {tt.lib.Paths[0]}} {
				args := processLibraryScanFanoutArgs(tt.lib, base, requested, tt.inventory)
				require.Len(t, args, len(tt.want))
				got := make([]string, 0, len(args))
				for _, arg := range args {
					require.Len(t, arg.ScopePaths, 1)
					require.NotEqual(t, tt.lib.Paths[0], arg.ScopePaths[0])
					got = append(got, arg.ScopePaths[0])
				}
				require.Equal(t, tt.want, got)
			}
		})
	}
}

// The scanner is a dumb per-owner-unit enqueuer: one process_scan job per
// artist / author / movie / show directory (or loose file). Grouping smarter
// than the directory structure — and all searching — happens downstream in
// the identify job, which skips the live search for already-known units via
// the persisted decisions overlay.
func TestProcessLibraryScanFanoutIsPerOwnerUnit(t *testing.T) {
	music := sqlc.Library{ID: 7, MediaType: sqlc.MediaTypeMusic, Paths: []string{"/storage/Music"}}
	musicInv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
		Root: "/storage/Music",
		Files: []scanner.InventoryFile{
			{Root: "/storage/Music", Path: "/storage/Music/Alpha/First/01.flac", RelPath: "Alpha/First/01.flac", Class: scanner.ClassPrimaryMedia},
			{Root: "/storage/Music", Path: "/storage/Music/Beta/Second/01.flac", RelPath: "Beta/Second/01.flac", Class: scanner.ClassPrimaryMedia},
			{Root: "/storage/Music", Path: "/storage/Music/Gamma/Third/01.flac", RelPath: "Gamma/Third/01.flac", Class: scanner.ClassPrimaryMedia},
		},
	}}}
	base := ProcessLibraryScanArgs{LibraryID: music.ID, Force: true}

	t.Run("music fans out one job per artist", func(t *testing.T) {
		for _, requested := range [][]string{nil, {"/storage/Music"}, {"/storage/Music/Alpha", "/storage/Music/Beta", "/storage/Music/Gamma"}} {
			args := processLibraryScanFanoutArgs(music, base, requested, musicInv)
			require.Len(t, args, 3)
			for i, want := range []string{"/storage/Music/Alpha", "/storage/Music/Beta", "/storage/Music/Gamma"} {
				require.Equal(t, []string{want}, args[i].ScopePaths)
			}
		}
	})

	t.Run("changed artist album collapses into its artist unit", func(t *testing.T) {
		args := processLibraryScanFanoutArgs(music, base, []string{
			"/storage/Music/Alpha",
			"/storage/Music/Alpha/First",
		}, musicInv)
		require.Len(t, args, 1)
		require.Equal(t, []string{"/storage/Music/Alpha"}, args[0].ScopePaths)
	})

	t.Run("books fan out per author directory", func(t *testing.T) {
		books := sqlc.Library{ID: 8, MediaType: sqlc.MediaTypeBook, Paths: []string{"/storage/Books"}}
		bookInv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
			Root: "/storage/Books",
			Files: []scanner.InventoryFile{
				{Root: "/storage/Books", Path: "/storage/Books/Frank Herbert/Dune (1965)/Dune.epub", RelPath: "Frank Herbert/Dune (1965)/Dune.epub", Class: scanner.ClassPrimaryMedia},
				{Root: "/storage/Books", Path: "/storage/Books/Frank Herbert/Dune Messiah (1969)/Dune Messiah.epub", RelPath: "Frank Herbert/Dune Messiah (1969)/Dune Messiah.epub", Class: scanner.ClassPrimaryMedia},
				{Root: "/storage/Books", Path: "/storage/Books/Andy Weir - Project Hail Mary (2021).epub", RelPath: "Andy Weir - Project Hail Mary (2021).epub", Class: scanner.ClassPrimaryMedia},
			},
		}}}
		args := processLibraryScanFanoutArgs(books, ProcessLibraryScanArgs{LibraryID: books.ID}, nil, bookInv)
		require.Len(t, args, 2)
		require.Equal(t, []string{"/storage/Books/Andy Weir - Project Hail Mary (2021).epub"}, args[0].ScopePaths, "a loose file at the root is its own unit")
		require.Equal(t, []string{"/storage/Books/Frank Herbert"}, args[1].ScopePaths, "both Dune books share the author unit")
	})

	t.Run("empty library produces no jobs", func(t *testing.T) {
		args := processLibraryScanFanoutArgs(music, base, nil, scanner.Inventory{})
		require.Empty(t, args)
	})
}

func TestProcessLibraryScanNeedsOwnerFanout(t *testing.T) {
	lib := sqlc.Library{ID: 7, MediaType: sqlc.MediaTypeMusic, Paths: []string{"/storage/Music"}}

	require.True(t, processLibraryScanNeedsOwnerFanout(lib, nil), "nil-scope jobs re-fan into owner units")
	require.True(t, processLibraryScanNeedsOwnerFanout(lib, []string{"/storage/Music"}), "library-root scopes re-fan into owner units")
	require.True(t, processLibraryScanNeedsOwnerFanout(lib, []string{"/storage/Music/Alpha", "/storage/Music/Beta"}), "legacy multi-owner batches split")
	require.False(t, processLibraryScanNeedsOwnerFanout(lib, []string{"/storage/Music/Alpha"}), "a single owner unit runs as-is")

	require.True(t, scannerScopesNeedInventoryExpansion(lib, nil), "whole-library re-fanout needs the inventory")
	require.True(t, scannerScopesNeedInventoryExpansion(lib, []string{"/storage/Music"}), "root expansion needs the inventory")
	require.False(t, scannerScopesNeedInventoryExpansion(lib, []string{"/storage/Music/Alpha", "/storage/Music/Beta"}), "plain splits skip the walk")
}

func TestOrphanedScannerRequeueArgsSplitPerOwnerScope(t *testing.T) {
	args := orphanedScannerRequeueArgs([]orphanedScannerEntity{
		{ID: 1, LibraryID: 5, ScopePaths: []string{"/storage/Music/Alpha", "/storage/Music/Beta"}},
		{ID: 2, LibraryID: 5, ScopePaths: []string{"/storage/Music/Beta"}},
		{ID: 3, LibraryID: 5, ScopePaths: nil},
	})

	require.Len(t, args, 3, "per-scope splits dedupe across entities; nil-scope requeues once")
	require.Equal(t, []string{"/storage/Music/Alpha"}, args[0].ScopePaths)
	require.Equal(t, []string{"/storage/Music/Beta"}, args[1].ScopePaths)
	require.Nil(t, args[2].ScopePaths, "legacy whole-library entities requeue as nil-scope for worker re-fanout")
	for _, a := range args {
		require.True(t, a.Force, "requeues bypass change detection")
		require.EqualValues(t, 5, a.LibraryID)
	}
}

func TestScannerRichMetadataTargetsAndDetail(t *testing.T) {
	detail := &metadata.MediaDetail{Title: "Dune"}
	result := scanner.Result{
		MovieApply: []scanner.MovieApplyResult{{
			Key:         "tmdb:438631",
			Action:      "applied",
			MediaItemID: 42,
		}, {
			Key:         "tmdb:999001",
			Action:      "skipped",
			MediaItemID: 43,
		}},
		MovieMetadata: []scanner.MovieFetchPreview{{
			Key:    "tmdb:438631",
			Detail: detail,
		}},
	}

	targets := scannerRichMetadataTargets(sqlc.Library{MediaType: sqlc.MediaTypeMovie}, result)
	require.Len(t, targets, 1)
	require.Equal(t, int64(42), targets[0].mediaItemID)
	require.Equal(t, metadata.KindMovie, targets[0].kind)

	got, kind, err := richMetadataDetailForJob(result, ApplyRichMetadataArgs{
		MediaKind: string(metadata.KindMovie),
		Key:       "tmdb:438631",
	})
	require.NoError(t, err)
	require.Equal(t, metadata.KindMovie, kind)
	require.Same(t, detail, got)
}

func TestLibraryScanProgressLabelIncludesScope(t *testing.T) {
	lib := sqlc.Library{Name: "Movies", Paths: []string{"/storage/Movies"}}

	require.Equal(t, "Movies", libraryScanProgressLabel(lib, nil))
	require.Equal(t, "Movies · The Matrix (1999)", libraryScanProgressLabel(lib, []string{"/storage/Movies/The Matrix (1999)"}))
	require.Equal(t, "Movies · The Matrix (1999) +1", libraryScanProgressLabel(lib, []string{
		"/storage/Movies/The Matrix (1999)",
		"/storage/Movies/Alien (1979)",
	}))
	require.Equal(t, "Movies · Loose Folder", libraryScanProgressLabel(lib, []string{"Loose Folder"}))
}

func TestLibraryFileNeedsProbe(t *testing.T) {
	require.True(t, libraryFileNeedsProbe(sqlc.LibraryFile{}))
	require.True(t, libraryFileNeedsProbe(sqlc.LibraryFile{MediaInfo: []byte("{}")}))
	require.True(t, libraryFileNeedsProbe(sqlc.LibraryFile{MediaInfo: []byte(" null ")}))
	require.False(t, libraryFileNeedsProbe(sqlc.LibraryFile{MediaInfo: []byte(`{"format":{}}`)}))
}

func TestLibraryFileHasVideo(t *testing.T) {
	require.False(t, libraryFileHasVideo(sqlc.LibraryFile{}))
	require.False(t, libraryFileHasVideo(sqlc.LibraryFile{MediaInfo: []byte(`{"streams":[{"codec_type":"audio"}]}`)}))
	require.True(t, libraryFileHasVideo(sqlc.LibraryFile{MediaInfo: []byte(`{"streams":[{"codec_type":"video"}]}`)}))
}

func TestScannerMediaTypeSideEffects(t *testing.T) {
	require.True(t, scannerMediaTypeFetchesRatings(sqlc.MediaTypeMovie))
	require.True(t, scannerMediaTypeFetchesRatings(sqlc.MediaTypeBook))
	require.False(t, scannerMediaTypeFetchesRatings(sqlc.MediaTypeMusic))

	require.True(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeMovie))
	require.True(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeTv))
	require.True(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeAnime))
	require.False(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeBook))

	require.True(t, scannerMediaTypeScansSegments(sqlc.MediaTypeMovie))
	require.True(t, scannerMediaTypeScansSegments(sqlc.MediaTypeTv))
	require.True(t, scannerMediaTypeScansSegments(sqlc.MediaTypeAnime))
	require.False(t, scannerMediaTypeScansSegments(sqlc.MediaTypeMusic))
}

func TestLibraryFileHasPrimaryLink(t *testing.T) {
	require.False(t, libraryFileHasPrimaryLink(nil))
	require.False(t, libraryFileHasPrimaryLink([]sqlc.LibraryFileLink{{RelationType: "extra"}}))
	require.True(t, libraryFileHasPrimaryLink([]sqlc.LibraryFileLink{{RelationType: "episode"}}))
	require.True(t, libraryFileHasPrimaryLink([]sqlc.LibraryFileLink{{RelationType: "part"}}))
}

func TestShouldSaveImageSidecar(t *testing.T) {
	require.True(t, ShouldSaveImageSidecar("poster", 0, ""))
	require.True(t, ShouldSaveImageSidecar("clearart", 0, ""))
	require.True(t, ShouldSaveImageSidecar("banner", 0, ""))
	require.True(t, ShouldSaveImageSidecar("logo", 0, ""))
	require.True(t, ShouldSaveImageSidecar("thumb", 0, ""))
	require.True(t, ShouldSaveImageSidecar("backdrop", 0, ""))
	require.True(t, ShouldSaveImageSidecar("backdrop", 4, "en"))

	require.False(t, ShouldSaveImageSidecar("poster", 1001, "season-1"))
	require.False(t, ShouldSaveImageSidecar("still", 2001, "s01e01"))
	require.False(t, ShouldSaveImageSidecar("logo", 1, ""))
	require.False(t, ShouldSaveImageSidecar("backdrop", 1000, "season-1"))
}

func TestTrackFileNeedsLoudness(t *testing.T) {
	require.True(t, trackFileNeedsLoudness(sqlc.TrackFile{}))
	require.True(t, trackFileNeedsLoudness(sqlc.TrackFile{
		IntegratedLufs: pgtype.Numeric{Valid: true},
	}))
	require.True(t, trackFileNeedsLoudness(sqlc.TrackFile{
		BoundariesAnalyzedAt: pgtype.Timestamptz{Valid: true},
	}))
	require.False(t, trackFileNeedsLoudness(sqlc.TrackFile{
		IntegratedLufs:       pgtype.Numeric{Valid: true},
		BoundariesAnalyzedAt: pgtype.Timestamptz{Valid: true},
	}))
}

// The scan-progress denominator lives in library_scan_bursts, maintained
// transactionally with each unit insert: a unit enqueued while the library
// is idle RESETS the row (a new burst); while other units are active it
// increments. The bursts row is locked FOR UPDATE, so concurrent first
// units serialize — exactly one resets, the rest increment.
func TestInsertScanUnitWithBurstResetsWhenIdleIncrementsWhenActive(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scan-burst-bump-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/media/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = 'process_scan' AND NULLIF(args->>'library_id','')::bigint = $1`, lib.ID)
	})

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)

	burstTotal := func() int64 {
		var n int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT units_total FROM library_scan_bursts WHERE library_id = $1`, lib.ID).Scan(&n))
		return n
	}

	// Seed a stale row from a "previous burst".
	_, err = pool.Exec(ctx, `INSERT INTO library_scan_bursts (library_id, units_total) VALUES ($1, 9000)`, lib.ID)
	require.NoError(t, err)

	// First unit of a new burst: library idle → reset (the row lock plus
	// pre-insert idle check make this exact, no self-exclusion needed).
	require.NoError(t, EnqueueProcessLibraryScan(ctx, rc, pool, ProcessLibraryScanArgs{
		LibraryID:  lib.ID,
		ScopePaths: []string{"/media/music/Alpha"},
	}, PriorityScan, ""))
	require.EqualValues(t, 1, burstTotal(), "first unit of a burst resets the stale total")
	var firstQueue string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT queue FROM river_job
		WHERE kind = 'process_scan'
		  AND NULLIF(args->>'library_id', '')::bigint = $1
		ORDER BY id
		LIMIT 1`, lib.ID).Scan(&firstQueue))
	require.Equal(t, "process_scan_music", firstQueue)

	// Second unit while the first is queued → increment.
	require.NoError(t, EnqueueProcessLibraryScan(ctx, rc, pool, ProcessLibraryScanArgs{
		LibraryID:  lib.ID,
		ScopePaths: []string{"/media/music/Beta"},
	}, PriorityScan, ""))
	require.EqualValues(t, 2, burstTotal(), "subsequent units increment")

	// A dedup'd duplicate insert must not bump the counter.
	require.NoError(t, EnqueueProcessLibraryScan(ctx, rc, pool, ProcessLibraryScanArgs{
		LibraryID:  lib.ID,
		ScopePaths: []string{"/media/music/Beta"},
	}, PriorityScan, ""))
	require.EqualValues(t, 2, burstTotal(), "unique-dedup'd inserts leave the counter untouched")
}
