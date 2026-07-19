package scanner

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestScannerAnalysisGenerationReconcilesScopeAndRejectsStaleSearch(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createLifecycleMovieLibrary(t, ctx, pool, q, "scanner-generation-reconcile")
	scope := []string{"/media/movies/Example (2024)"}
	opts := Options{ScopePaths: scope}

	oldResult := lifecycleMovieResult("title_year:example|2024", "Example", "heya:movie:example")
	first, err := PersistScannerAnalysisEntities(ctx, pool, lib, opts, oldResult)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.EqualValues(t, 1, first[0].Entity.PipelineGeneration)
	require.Equal(t, first[0].Entity.PipelineGeneration, first[0].Artifact.PipelineGeneration)
	require.Equal(t, first[0].Artifact.ID, first[0].Entity.AnalysisArtifactID.Int64)

	oldReview := upsertLifecycleIdentity(t, ctx, q, lib, "title_year:example|2024", "needs_review", "")
	acceptedHistory := upsertLifecycleIdentity(t, ctx, q, lib, "title_year:accepted history|2020", "accepted", "heya:movie:history")

	second, err := PersistScannerAnalysisEntities(ctx, pool, lib, opts, oldResult)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, first[0].Entity.ID, second[0].Entity.ID)
	require.EqualValues(t, 2, second[0].Entity.PipelineGeneration)
	require.Equal(t, second[0].Entity.PipelineGeneration, second[0].Artifact.PipelineGeneration)

	_, _, current, err := LoadCurrentScannerEntityArtifactResult(ctx, pool, first[0].Entity.ID, first[0].Artifact.ID, scanArtifactKindAnalyze)
	require.NoError(t, err)
	require.False(t, current, "generation one analysis must no longer be current")
	_, current, err = PersistScannerSearchEntity(ctx, pool, lib, opts, first[0].Entity.ID, first[0].Artifact.ID, oldResult, 0)
	require.NoError(t, err)
	require.False(t, current, "a stale search worker exits successfully without attaching an artifact")
	var scanRunsBefore, scanRunsAfter int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM scan_runs WHERE library_id = $1`, lib.ID).Scan(&scanRunsBefore))
	run := NewLibraryRun(lib, Options{
		PersistenceDB: pool, PersistScan: true, RemoteSearch: true, ScopePaths: scope,
	}, io.Discard)
	run.result = oldResult
	_, _, current, err = run.FinishSearchEntity(ctx, first[0].Entity.ID, first[0].Artifact.ID)
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM scan_runs WHERE library_id = $1`, lib.ID).Scan(&scanRunsAfter))
	require.Equal(t, scanRunsBefore, scanRunsAfter, "stale stage finalization must CAS before writing global identities, candidates, findings, or scan runs")
	entity, err := q.GetScannerEntity(ctx, second[0].Entity.ID)
	require.NoError(t, err)
	require.Equal(t, second[0].Artifact.ID, entity.AnalysisArtifactID.Int64)
	require.False(t, entity.SearchArtifactID.Valid)

	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET status = 'applying' WHERE id = $1`, entity.ID)
	require.NoError(t, err)
	_, err = PersistScannerAnalysisEntities(ctx, pool, lib, opts, oldResult)
	require.ErrorIs(t, err, ErrScannerScopeApplying)
	entity, err = q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, entity.PipelineGeneration, "the applying barrier must leave the generation untouched")
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET status = 'discovered' WHERE id = $1`, entity.ID)
	require.NoError(t, err)

	newResult := lifecycleMovieResult("title_year:renamed example|2024", "Renamed Example", "heya:movie:renamed")
	renamed, err := PersistScannerAnalysisEntities(ctx, pool, lib, opts, newResult)
	require.NoError(t, err)
	require.Len(t, renamed, 1)
	require.NotEqual(t, entity.ID, renamed[0].Entity.ID)
	_, err = q.GetScannerEntity(ctx, entity.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the complete scope snapshot removes identities absent from the new analysis")
	_, err = q.GetScannerIdentityForView(ctx, sqlc.GetScannerIdentityForViewParams{LibraryID: lib.ID, IdentityID: oldReview.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "unclaimed review rows must not poison dashboard counts after a rename")
	_, err = q.GetScannerIdentityForView(ctx, sqlc.GetScannerIdentityForViewParams{LibraryID: lib.ID, IdentityID: acceptedHistory.ID})
	require.NoError(t, err, "accepted history is retained even when no scanner entity currently claims it")

	newReview := upsertLifecycleIdentity(t, ctx, q, lib, "title_year:renamed example|2024", "needs_review", "")
	empty, err := PersistScannerAnalysisEntities(ctx, pool, lib, opts, Result{})
	require.NoError(t, err)
	require.Empty(t, empty)
	_, err = q.GetScannerEntity(ctx, renamed[0].Entity.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetScannerIdentityForView(ctx, sqlc.GetScannerIdentityForViewParams{LibraryID: lib.ID, IdentityID: newReview.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "an empty scope snapshot prunes its unclaimed review row")
}

func TestAnalysisHandoffFailureRollsBackNewGeneration(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createLifecycleMovieLibrary(t, ctx, pool, q, "scanner-analysis-handoff-rollback")
	opts := Options{ScopePaths: []string{"/media/movies/Example (2024)"}}
	result := lifecycleMovieResult("title_year:example|2024", "Example", "heya:movie:example")
	first, err := PersistScannerAnalysisEntities(ctx, pool, lib, opts, result)
	require.NoError(t, err)
	require.Len(t, first, 1)

	sentinel := errors.New("downstream search insert failed")
	_, err = PersistScannerAnalysisEntitiesWithHandoff(ctx, pool, lib, opts, result, func(context.Context, pgx.Tx, []ScannerEntityRef) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	entity, err := q.GetScannerEntity(ctx, first[0].Entity.ID)
	require.NoError(t, err)
	require.Equal(t, first[0].Entity.PipelineGeneration, entity.PipelineGeneration)
	require.Equal(t, first[0].Artifact.ID, entity.AnalysisArtifactID.Int64)
}

func TestScannerSearchDecisionsIgnoreIdentitySharedAcrossScopes(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createLifecycleMovieLibrary(t, ctx, pool, q, "scanner-ambiguous-decision")
	const identityKey = "title_year:shared|2024"

	upsertLifecycleIdentity(t, ctx, q, lib, identityKey, "accepted", "heya:movie:shared")
	for _, scope := range []string{"scope:one", "scope:two"} {
		_, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
			LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: scope,
			ScopePaths: []string{"/media/movies/" + scope}, IdentityKey: identityKey,
			Title: "Shared", Year: "2024", Status: "discovered", Data: []byte("{}"),
		})
		require.NoError(t, err)
	}

	decisions, err := LoadScannerSearchDecisions(ctx, pool, lib)
	require.NoError(t, err)
	require.NotContains(t, decisions, identityKey, "a library-global decision is unsafe when the identity occurs in multiple scopes")

	_, err = pool.Exec(ctx, `DELETE FROM scanner_entities WHERE library_id = $1 AND media_type = $2 AND scope_key = 'scope:two'`, lib.ID, lib.MediaType)
	require.NoError(t, err)
	decisions, err = LoadScannerSearchDecisions(ctx, pool, lib)
	require.NoError(t, err)
	require.Contains(t, decisions, identityKey)
}

func TestScannerSearchDecisionRevisionExpiresAutomaticButPreservesManual(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createLifecycleMovieLibrary(t, ctx, pool, q, "scanner-decision-revision")
	const identityKey = "title_year:revisioned|2024"

	upsertLifecycleIdentity(t, ctx, q, lib, identityKey, "accepted", "heya:movie:automatic")
	rows, err := q.ListScannerSearchDecisionsByLibrary(ctx, sqlc.ListScannerSearchDecisionsByLibraryParams{
		LibraryID: lib.ID, MediaType: lib.MediaType,
		ReviewStatuses:  []string{"accepted", "rejected", "ignored"},
		MatcherRevision: scannerSearchMatcherRevision + 1,
	})
	require.NoError(t, err)
	require.Empty(t, rows, "an automatic accept from an older matcher revision must be reconsidered")

	reconsidered, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, IdentityKey: identityKey,
		Title: "Revisioned", Year: "2024", Confidence: 1, Source: "scanner",
		ReviewStatus: "needs_review", MetadataProviderID: "",
		RawIdentity: []byte("{}"), DecisionMatcherRevision: scannerSearchMatcherRevision + 1,
	})
	require.NoError(t, err)
	require.Equal(t, "needs_review", reconsidered.ReviewStatus)
	require.Empty(t, reconsidered.MetadataProviderID, "a reconsidered automatic decision must clear its obsolete provider")
	identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, IdentityKey: identityKey,
		Title: "Revisioned", Year: "2024", Confidence: 1, Source: "scanner",
		ReviewStatus: "accepted", MetadataProviderID: "heya:movie:automatic-v2",
		RawIdentity: []byte("{}"), DecisionMatcherRevision: scannerSearchMatcherRevision + 1,
	})
	require.NoError(t, err)

	rejected, err := q.RejectScannerIdentity(ctx, sqlc.RejectScannerIdentityParams{
		LibraryID: lib.ID, IdentityID: identity.ID, Reason: "manual_rejected",
	})
	require.NoError(t, err)
	require.Equal(t, "manual", rejected.DecisionProvenance)
	require.Equal(t, "rejected", rejected.ReviewStatus)

	manual, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, IdentityKey: identityKey,
		Title: "Revisioned", Year: "2024", Confidence: 1, Source: "scanner",
		ReviewStatus: "accepted", MetadataProviderID: "heya:movie:new-automatic",
		RawIdentity: []byte("{}"), DecisionMatcherRevision: scannerSearchMatcherRevision + 1,
	})
	require.NoError(t, err)
	require.Equal(t, "manual", manual.DecisionProvenance)
	require.Equal(t, "rejected", manual.ReviewStatus, "automatic persistence cannot overwrite a manual decision")
	require.Equal(t, "heya:movie:automatic-v2", manual.MetadataProviderID)

	rows, err = q.ListScannerSearchDecisionsByLibrary(ctx, sqlc.ListScannerSearchDecisionsByLibraryParams{
		LibraryID: lib.ID, MediaType: lib.MediaType,
		ReviewStatuses:  []string{"accepted", "rejected", "ignored"},
		MatcherRevision: scannerSearchMatcherRevision + 99,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1, "manual decisions are independent of matcher revision")
	require.Equal(t, "rejected", rows[0].ReviewStatus)
}

func TestManualRejectionAtomicallyInvalidatesInFlightEntityArtifacts(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createLifecycleMovieLibrary(t, ctx, pool, q, "scanner-manual-reject-invalidation")
	const key = "title_year:race|2026"
	identity := upsertLifecycleIdentity(t, ctx, q, lib, key, "accepted", "heya:movie:old")
	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope", ScopePaths: []string{"/media/movies/Race"},
		IdentityKey: key, Title: "Race", Year: "2026", ProviderID: "heya:movie:old", Status: "discovered", Data: []byte("{}"),
	})
	require.NoError(t, err)
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: scanArtifactKindFetch, SchemaVersion: 1, Data: []byte("{}"), PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET status = 'fetched', metadata_artifact_id = $2 WHERE id = $1`, entity.ID, artifact.ID)
	require.NoError(t, err)

	_, err = q.RejectScannerIdentity(ctx, sqlc.RejectScannerIdentityParams{LibraryID: lib.ID, IdentityID: identity.ID, Reason: "manual_rejected"})
	require.NoError(t, err)
	invalidated, err := q.GetScannerEntity(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, entity.PipelineGeneration+1, invalidated.PipelineGeneration)
	require.Equal(t, "discovered", invalidated.Status)
	require.False(t, invalidated.MetadataArtifactID.Valid)

	err = ValidateCurrentScannerEntityArtifact(ctx, pool, entity.ID, artifact.ID, scanArtifactKindFetch)
	var stale *ArtifactReplayError
	require.ErrorAs(t, err, &stale, "an apply paused before commit must observe the manual rejection")
}

func TestTransactionalApplyLineageGuardHoldsReviewMutationUntilCommit(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib := createLifecycleMovieLibrary(t, ctx, pool, q, "scanner-transactional-apply-guard")
	entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, ScopeKey: "scope", ScopePaths: []string{"/media/movies/Race"},
		IdentityKey: "title_year:guard|2026", Title: "Guard", Year: "2026", Status: "discovered", Data: []byte("{}"),
	})
	require.NoError(t, err)
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID: entity.ID, Stage: scanArtifactKindFetch, SchemaVersion: 1,
		Data: []byte("{}"), PipelineGeneration: entity.PipelineGeneration,
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE scanner_entities SET status = 'fetched', metadata_artifact_id = $2 WHERE id = $1`, entity.ID, artifact.ID)
	require.NoError(t, err)

	applyTx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer applyTx.Rollback(ctx) //nolint:errcheck // no-op after commit
	require.NoError(t, ValidateCurrentScannerEntityArtifactTx(ctx, applyTx, entity.ID, artifact.ID, scanArtifactKindFetch))

	started := make(chan struct{})
	updated := make(chan error, 1)
	go func() {
		close(started)
		_, updateErr := pool.Exec(ctx, `
			UPDATE scanner_entities
			SET pipeline_generation = pipeline_generation + 1,
			    status = 'discovered', metadata_artifact_id = NULL
			WHERE id = $1
		`, entity.ID)
		updated <- updateErr
	}()
	<-started
	select {
	case updateErr := <-updated:
		require.NoError(t, updateErr)
		t.Fatal("manual review mutation crossed the domain apply commit guard")
	case <-time.After(100 * time.Millisecond):
		// Expected: the final guard's entity lock remains owned by applyTx.
	}
	require.NoError(t, applyTx.Commit(ctx))
	select {
	case updateErr := <-updated:
		require.NoError(t, updateErr)
	case <-time.After(5 * time.Second):
		t.Fatal("manual review mutation did not resume after apply committed")
	}
}

func createLifecycleMovieLibrary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q *sqlc.Queries, name string) sqlc.Library {
	t.Helper()
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: name, MediaType: sqlc.MediaTypeMovie, Paths: []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	return lib
}

func lifecycleMovieResult(key, title, providerID string) Result {
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root: "/media/movies", Path: "/media/movies/Example (2024)/Example.mkv",
				RelPath: "Example (2024)/Example.mkv", Name: "Example.mkv", Class: ClassPrimaryMedia,
			}},
		}}},
		MovieMatches: []MovieMatch{{Key: key, Title: title, Year: "2024", Files: []string{"Example (2024)/Example.mkv"}}},
	}
	if providerID != "" {
		result.MovieSearch = []MovieSearchMatch{{
			Key: key, Query: MovieSearchQuery{Title: title, Year: "2024"}, Accepted: true,
			ProviderID: providerID, Title: title, Year: "2024", Confidence: 1,
		}}
	}
	return result
}

func upsertLifecycleIdentity(t *testing.T, ctx context.Context, q *sqlc.Queries, lib sqlc.Library, key, status, providerID string) sqlc.LocalMediaIdentity {
	t.Helper()
	identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, IdentityKey: key,
		Title: key, Year: "2024", Confidence: 1, Source: "scanner",
		ReviewStatus: status, MetadataProviderID: providerID,
		RawIdentity: []byte("{}"), DecisionMatcherRevision: scannerSearchMatcherRevision,
	})
	require.NoError(t, err)
	return identity
}
