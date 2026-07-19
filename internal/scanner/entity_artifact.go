package scanner

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
)

const (
	scanArtifactKindApply = "apply_result"
)

var ErrScannerScopeApplying = errors.New("scanner scope has an entity being applied")

type ScannerEntityRef struct {
	Entity       sqlc.ScannerEntity
	Artifact     sqlc.ScannerEntityArtifact
	Accepted     bool
	ProviderID   string
	IdentityKey  string
	ReviewStatus string
}

type ScannerAnalysisHandoff func(context.Context, pgx.Tx, []ScannerEntityRef) error
type ScannerSearchHandoff func(context.Context, pgx.Tx, ScannerEntityRef) error
type ScannerFetchHandoff func(context.Context, pgx.Tx, sqlc.ScannerEntityArtifact, Result) error

type scannerEntityDraft struct {
	IdentityKey string
	Title       string
	Year        string
	ProviderID  string
	Accepted    bool
	Status      string
	Data        any
}

type scannerStageScanPersistence struct {
	Lib       sqlc.Library
	Events    []Event
	Options   Options
	Summary   map[string]any
	ScanRunID int64
}

// PersistScannerAnalysisEntities stores one narrow local-analysis artifact per
// canonical owner candidate. One transaction replaces the complete entity set
// for the scope, advances every surviving entity's generation, and attaches
// the new analysis artifacts. This prevents a partial analysis persistence
// from mixing generations after a crash.
func PersistScannerAnalysisEntities(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, result Result) ([]ScannerEntityRef, error) {
	return persistScannerAnalysisEntities(ctx, db, lib, opts, result, nil)
}

// PersistScannerAnalysisEntitiesWithHandoff commits the complete analysis
// generation and every downstream queue insert together. A River failure
// therefore leaves neither half visible and the process job can retry safely.
func PersistScannerAnalysisEntitiesWithHandoff(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, result Result, handoff ScannerAnalysisHandoff) ([]ScannerEntityRef, error) {
	return persistScannerAnalysisEntities(ctx, db, lib, opts, result, handoff)
}

func persistScannerAnalysisEntities(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, result Result, handoff ScannerAnalysisHandoff) ([]ScannerEntityRef, error) {
	if db == nil {
		return nil, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin scanner analysis persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	scopeKey := scannerScopeKey(opts.ScopePaths)
	lockKey := fmt.Sprintf("scanner-scope:%d:%s:%s", lib.ID, lib.MediaType, scopeKey)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return nil, fmt.Errorf("lock scanner scope: %w", err)
	}
	q := sqlc.New(tx)
	existing, err := q.ListScannerEntitiesForScopeForUpdate(ctx, sqlc.ListScannerEntitiesForScopeForUpdateParams{
		LibraryID: lib.ID,
		MediaType: lib.MediaType,
		ScopeKey:  scopeKey,
	})
	if err != nil {
		return nil, fmt.Errorf("lock scanner scope entities: %w", err)
	}
	for _, entity := range existing {
		if entity.Status == "applying" {
			return nil, ErrScannerScopeApplying
		}
	}

	result = filterResultToScopes(result, opts.ScopePaths, nil)
	// Capture the complete owner-scope set before narrowing each entity. The
	// narrow artifact can prove its existing files, but only this shared guard
	// can notice a newly added NFO/.plexmatch/artwork/media source that did not
	// exist during analysis.
	result.artifactSourceSet = sourceSetFromInventory(result.Inventory, opts.ScopePaths)
	drafts := scannerEntityDrafts(lib, result)
	refs := make([]ScannerEntityRef, 0, len(drafts))
	identityKeys := make([]string, 0, len(drafts))
	seen := make(map[string]struct{}, len(drafts))
	for _, draft := range drafts {
		if _, duplicate := seen[draft.IdentityKey]; duplicate {
			return nil, fmt.Errorf("scanner analysis produced duplicate identity key %q", draft.IdentityKey)
		}
		seen[draft.IdentityKey] = struct{}{}
		identityKeys = append(identityKeys, draft.IdentityKey)

		narrow := filterResultToIdentityKey(result, draft.IdentityKey)
		data, err := marshalResultArtifact(scanArtifactKindAnalyze, opts, narrow)
		if err != nil {
			return nil, err
		}
		entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
			LibraryID:        lib.ID,
			MediaType:        lib.MediaType,
			ScopeKey:         scopeKey,
			ScopePaths:       normalizedScopePaths(opts.ScopePaths),
			IdentityKey:      draft.IdentityKey,
			Title:            draft.Title,
			Year:             draft.Year,
			ProviderID:       "",
			Status:           "discovered",
			SearchScanRunID:  pgtypeZeroInt8(),
			SearchArtifactID: pgtypeZeroInt8(),
			ErrorMessage:     "",
			Data:             mustJSONBytes(draft.Data),
		})
		if err != nil {
			return nil, fmt.Errorf("upsert scanner entity %s: %w", draft.IdentityKey, err)
		}
		artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
			EntityID:           entity.ID,
			Stage:              scanArtifactKindAnalyze,
			SchemaVersion:      scanArtifactSchemaV1,
			ScanRunID:          pgtypeZeroInt8(),
			Data:               data,
			PipelineGeneration: entity.PipelineGeneration,
			SourceArtifactID:   pgtypeZeroInt8(),
		})
		if err != nil {
			return nil, fmt.Errorf("persist scanner analysis artifact %s: %w", draft.IdentityKey, err)
		}
		entity, err = q.AttachScannerEntityAnalysisArtifact(ctx, sqlc.AttachScannerEntityAnalysisArtifactParams{
			AnalysisArtifactID: pgInt8(artifact.ID),
			EntityID:           entity.ID,
			PipelineGeneration: entity.PipelineGeneration,
		})
		if err != nil {
			return nil, fmt.Errorf("attach scanner analysis artifact %s: %w", draft.IdentityKey, err)
		}
		refs = append(refs, ScannerEntityRef{
			Entity:       entity,
			Artifact:     artifact,
			IdentityKey:  draft.IdentityKey,
			ReviewStatus: entity.Status,
		})
	}
	if _, err := q.DeleteScannerEntitiesForScopeExcept(ctx, sqlc.DeleteScannerEntitiesForScopeExceptParams{
		LibraryID:    lib.ID,
		MediaType:    lib.MediaType,
		ScopeKey:     scopeKey,
		IdentityKeys: identityKeys,
	}); err != nil {
		return nil, fmt.Errorf("reconcile scanner scope entities: %w", err)
	}
	if _, err := q.PruneUnclaimedScannerReviewIdentities(ctx, sqlc.PruneUnclaimedScannerReviewIdentitiesParams{
		LibraryID: lib.ID,
		MediaType: lib.MediaType,
	}); err != nil {
		return nil, fmt.Errorf("prune unclaimed scanner review identities: %w", err)
	}
	if handoff != nil {
		if err := handoff(ctx, tx, refs); err != nil {
			return nil, fmt.Errorf("scanner analysis handoff: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit scanner analysis persistence: %w", err)
	}
	return refs, nil
}

// PersistScannerSearchEntity attaches a search result only if its analysis
// artifact is still the current hand-off for the entity. A false current
// result is a successful no-op: a newer generation has superseded this job.
func PersistScannerSearchEntity(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, entityID, expectedAnalysisArtifactID int64, result Result, scanRunID int64) (ScannerEntityRef, bool, error) {
	return persistScannerSearchEntity(ctx, db, lib, opts, entityID, expectedAnalysisArtifactID, result, scanRunID, nil, nil)
}

func PersistScannerSearchEntityWithHandoff(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, entityID, expectedAnalysisArtifactID int64, result Result, scanRunID int64, handoff ScannerSearchHandoff) (ScannerEntityRef, bool, error) {
	return persistScannerSearchEntity(ctx, db, lib, opts, entityID, expectedAnalysisArtifactID, result, scanRunID, nil, handoff)
}

func persistScannerSearchEntity(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, entityID, expectedAnalysisArtifactID int64, result Result, scanRunID int64, scan *scannerStageScanPersistence, handoff ScannerSearchHandoff) (ScannerEntityRef, bool, error) {
	if db == nil {
		return ScannerEntityRef{}, false, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return ScannerEntityRef{}, false, fmt.Errorf("begin scanner search persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlc.New(tx)
	analysisArtifact, err := q.GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: expectedAnalysisArtifactID, Stage: scanArtifactKindAnalyze,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ScannerEntityRef{}, false, nil
	}
	if err != nil {
		return ScannerEntityRef{}, false, fmt.Errorf("load current scanner analysis artifact: %w", err)
	}
	entity, err := q.GetScannerEntity(ctx, entityID)
	if err != nil {
		return ScannerEntityRef{}, false, fmt.Errorf("load scanner entity for search: %w", err)
	}
	if entity.LibraryID != lib.ID || entity.MediaType != lib.MediaType || entity.ScopeKey != scannerScopeKey(opts.ScopePaths) {
		return ScannerEntityRef{}, false, fmt.Errorf("scanner search entity %d does not belong to requested scope", entityID)
	}
	result = filterResultToScopes(result, opts.ScopePaths, nil)
	var draft *scannerEntityDraft
	for _, candidate := range scannerEntityDrafts(lib, result) {
		if candidate.IdentityKey == entity.IdentityKey {
			copy := candidate
			draft = &copy
			break
		}
	}
	if draft == nil {
		return ScannerEntityRef{}, false, fmt.Errorf("scanner search result omitted identity key %q", entity.IdentityKey)
	}
	if scan != nil {
		if err := ValidateScannerArtifactSourcesWithDB(ctx, db, analysisArtifact); err != nil {
			return ScannerEntityRef{}, false, err
		}
		scanRunID, err = persistScanResultTx(ctx, q, scan.Lib, result, scan.Events, scan.Options, scan.Summary)
		if err != nil {
			return ScannerEntityRef{}, false, err
		}
		scan.ScanRunID = scanRunID
	}
	narrow := filterResultToIdentityKey(result, entity.IdentityKey)
	data, err := marshalResultArtifact(scanArtifactKindSearch, opts, narrow)
	if err != nil {
		return ScannerEntityRef{}, false, err
	}
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID:           entityID,
		Stage:              scanArtifactKindSearch,
		SchemaVersion:      scanArtifactSchemaV1,
		ScanRunID:          pgInt8(scanRunID),
		Data:               data,
		PipelineGeneration: analysisArtifact.PipelineGeneration,
		SourceArtifactID:   pgInt8(expectedAnalysisArtifactID),
	})
	if err != nil {
		return ScannerEntityRef{}, false, fmt.Errorf("persist scanner search artifact: %w", err)
	}
	entity, err = q.MarkScannerEntitySearched(ctx, sqlc.MarkScannerEntitySearchedParams{
		Title:                      draft.Title,
		Year:                       draft.Year,
		ProviderID:                 draft.ProviderID,
		Status:                     draft.Status,
		SearchScanRunID:            pgInt8(scanRunID),
		SearchArtifactID:           pgInt8(artifact.ID),
		ErrorMessage:               "",
		Data:                       mustJSONBytes(draft.Data),
		EntityID:                   entityID,
		PipelineGeneration:         analysisArtifact.PipelineGeneration,
		ExpectedAnalysisArtifactID: pgInt8(expectedAnalysisArtifactID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ScannerEntityRef{}, false, nil
	}
	if err != nil {
		return ScannerEntityRef{}, false, fmt.Errorf("mark scanner entity searched: %w", err)
	}
	ref := ScannerEntityRef{
		Entity: entity, Artifact: artifact, Accepted: draft.Accepted,
		ProviderID: draft.ProviderID, IdentityKey: draft.IdentityKey, ReviewStatus: draft.Status,
	}
	if handoff != nil {
		if err := handoff(ctx, tx, ref); err != nil {
			return ScannerEntityRef{}, false, fmt.Errorf("scanner search handoff: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ScannerEntityRef{}, false, fmt.Errorf("commit scanner search persistence: %w", err)
	}
	return ref, true, nil
}

func BeginScannerEntityFetch(ctx context.Context, db *pgxpool.Pool, entityID, expectedSearchArtifactID int64) (bool, error) {
	if db == nil {
		return false, nil
	}
	q := sqlc.New(db)
	artifact, err := q.GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: expectedSearchArtifactID, Stage: scanArtifactKindSearch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load current scanner search artifact: %w", err)
	}
	_, err = q.MarkScannerEntityFetching(ctx, sqlc.MarkScannerEntityFetchingParams{
		EntityID:                 entityID,
		PipelineGeneration:       artifact.PipelineGeneration,
		ExpectedSearchArtifactID: pgInt8(expectedSearchArtifactID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mark scanner entity fetching: %w", err)
	}
	return true, nil
}

func PersistScannerFetchEntity(ctx context.Context, db *pgxpool.Pool, entityID, expectedSearchArtifactID int64, result Result, scanRunID int64) (sqlc.ScannerEntityArtifact, bool, error) {
	return persistScannerFetchEntity(ctx, db, entityID, expectedSearchArtifactID, result, scanRunID, nil, nil)
}

func PersistScannerFetchEntityWithHandoff(ctx context.Context, db *pgxpool.Pool, entityID, expectedSearchArtifactID int64, result Result, scanRunID int64, handoff ScannerFetchHandoff) (sqlc.ScannerEntityArtifact, bool, error) {
	return persistScannerFetchEntity(ctx, db, entityID, expectedSearchArtifactID, result, scanRunID, nil, handoff)
}

func persistScannerFetchEntity(ctx context.Context, db *pgxpool.Pool, entityID, expectedSearchArtifactID int64, result Result, scanRunID int64, scan *scannerStageScanPersistence, handoff ScannerFetchHandoff) (sqlc.ScannerEntityArtifact, bool, error) {
	if db == nil {
		return sqlc.ScannerEntityArtifact{}, false, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, false, fmt.Errorf("begin scanner fetch persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlc.New(tx)
	searchArtifact, err := q.GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: expectedSearchArtifactID, Stage: scanArtifactKindSearch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.ScannerEntityArtifact{}, false, nil
	}
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, false, fmt.Errorf("load current scanner search artifact: %w", err)
	}
	if scan != nil {
		if err := ValidateScannerArtifactSourcesWithDB(ctx, db, searchArtifact); err != nil {
			return sqlc.ScannerEntityArtifact{}, false, err
		}
		scanRunID, err = persistScanResultTx(ctx, q, scan.Lib, result, scan.Events, scan.Options, scan.Summary)
		if err != nil {
			return sqlc.ScannerEntityArtifact{}, false, err
		}
		scan.ScanRunID = scanRunID
	}
	data, err := marshalResultArtifact(scanArtifactKindFetch, Options{}, result)
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, false, err
	}
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID:           entityID,
		Stage:              scanArtifactKindFetch,
		SchemaVersion:      scanArtifactSchemaV1,
		ScanRunID:          pgInt8(scanRunID),
		Data:               data,
		PipelineGeneration: searchArtifact.PipelineGeneration,
		SourceArtifactID:   pgInt8(expectedSearchArtifactID),
	})
	if err != nil {
		return artifact, false, fmt.Errorf("persist scanner fetch artifact: %w", err)
	}
	status, msg := scannerFetchEntityStatus(result)
	_, err = q.MarkScannerEntityFetched(ctx, sqlc.MarkScannerEntityFetchedParams{
		Status:                   status,
		FetchScanRunID:           pgInt8(scanRunID),
		MetadataArtifactID:       pgInt8(artifact.ID),
		ErrorMessage:             msg,
		EntityID:                 entityID,
		PipelineGeneration:       searchArtifact.PipelineGeneration,
		ExpectedSearchArtifactID: pgInt8(expectedSearchArtifactID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.ScannerEntityArtifact{}, false, nil
	}
	if err != nil {
		return artifact, false, fmt.Errorf("mark scanner entity fetched: %w", err)
	}
	if handoff != nil {
		if err := handoff(ctx, tx, artifact, result); err != nil {
			return sqlc.ScannerEntityArtifact{}, false, fmt.Errorf("scanner fetch handoff: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return artifact, false, fmt.Errorf("commit scanner fetch persistence: %w", err)
	}
	return artifact, true, nil
}

func BeginScannerEntityApply(ctx context.Context, db *pgxpool.Pool, entityID, expectedMetadataArtifactID int64) (bool, error) {
	if db == nil {
		return false, nil
	}
	q := sqlc.New(db)
	artifact, err := q.GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: expectedMetadataArtifactID, Stage: scanArtifactKindFetch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load current scanner metadata artifact: %w", err)
	}
	_, err = q.MarkScannerEntityApplying(ctx, sqlc.MarkScannerEntityApplyingParams{
		EntityID:                   entityID,
		PipelineGeneration:         artifact.PipelineGeneration,
		ExpectedMetadataArtifactID: pgInt8(expectedMetadataArtifactID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mark scanner entity applying: %w", err)
	}
	return true, nil
}

func PersistScannerApplyEntity(ctx context.Context, db *pgxpool.Pool, entityID, expectedMetadataArtifactID int64, result Result, scanRunID int64) (sqlc.ScannerEntityArtifact, bool, error) {
	return persistScannerApplyEntity(ctx, db, entityID, expectedMetadataArtifactID, result, scanRunID, nil, false)
}

func persistScannerApplyEntity(ctx context.Context, db *pgxpool.Pool, entityID, expectedMetadataArtifactID int64, result Result, scanRunID int64, scan *scannerStageScanPersistence, pendingFanout bool) (sqlc.ScannerEntityArtifact, bool, error) {
	if db == nil {
		return sqlc.ScannerEntityArtifact{}, false, nil
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, false, fmt.Errorf("begin scanner apply persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlc.New(tx)
	metadataArtifact, err := q.GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: expectedMetadataArtifactID, Stage: scanArtifactKindFetch,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.ScannerEntityArtifact{}, false, nil
	}
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, false, fmt.Errorf("load current scanner metadata artifact: %w", err)
	}
	if scan != nil {
		if err := ValidateScannerArtifactSourcesWithDB(ctx, db, metadataArtifact); err != nil {
			return sqlc.ScannerEntityArtifact{}, false, err
		}
		scanRunID, err = persistScanResultTx(ctx, q, scan.Lib, result, scan.Events, scan.Options, scan.Summary)
		if err != nil {
			return sqlc.ScannerEntityArtifact{}, false, err
		}
		scan.ScanRunID = scanRunID
	}
	data, err := marshalResultArtifact(scanArtifactKindApply, Options{}, result)
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, false, err
	}
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID:           entityID,
		Stage:              scanArtifactKindApply,
		SchemaVersion:      scanArtifactSchemaV1,
		ScanRunID:          pgInt8(scanRunID),
		Data:               data,
		PipelineGeneration: metadataArtifact.PipelineGeneration,
		SourceArtifactID:   pgInt8(expectedMetadataArtifactID),
	})
	if err != nil {
		return artifact, false, fmt.Errorf("persist scanner apply artifact: %w", err)
	}
	status, msg := scannerApplyEntityStatus(result)
	if pendingFanout {
		// The durable apply artifact is the hand-off between core database
		// materialization and River fanout. Keep the entity in applying until
		// every downstream job and the terminal status commit atomically.
		status, msg = "applying", ""
	}
	_, err = q.MarkScannerEntityApplied(ctx, sqlc.MarkScannerEntityAppliedParams{
		Status:                     status,
		ApplyArtifactID:            pgInt8(artifact.ID),
		ErrorMessage:               msg,
		EntityID:                   entityID,
		PipelineGeneration:         metadataArtifact.PipelineGeneration,
		ExpectedMetadataArtifactID: pgInt8(expectedMetadataArtifactID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.ScannerEntityArtifact{}, false, nil
	}
	if err != nil {
		return artifact, false, fmt.Errorf("mark scanner entity applied: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return artifact, false, fmt.Errorf("commit scanner apply persistence: %w", err)
	}
	return artifact, true, nil
}

// FinalizeScannerApplyEntityTx transitions a durable apply checkpoint to its
// terminal state. Callers commit this in the same transaction as all River
// fanout inserts so a crash can leave either the whole hand-off pending or the
// whole hand-off complete, never an applied entity with missing jobs.
func FinalizeScannerApplyEntityTx(ctx context.Context, tx pgx.Tx, entityID, expectedMetadataArtifactID, applyArtifactID int64, result Result) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("finalize scanner apply: transaction is nil")
	}
	q := sqlc.New(tx)
	artifact, err := q.GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: applyArtifactID, Stage: scanArtifactKindApply,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load current scanner apply artifact: %w", err)
	}
	if !artifact.SourceArtifactID.Valid || artifact.SourceArtifactID.Int64 != expectedMetadataArtifactID {
		return false, fmt.Errorf("scanner apply artifact %d does not descend from metadata artifact %d", applyArtifactID, expectedMetadataArtifactID)
	}
	status, msg := scannerApplyEntityStatus(result)
	_, err = q.MarkScannerEntityApplied(ctx, sqlc.MarkScannerEntityAppliedParams{
		Status:                     status,
		ApplyArtifactID:            pgInt8(applyArtifactID),
		ErrorMessage:               msg,
		EntityID:                   entityID,
		PipelineGeneration:         artifact.PipelineGeneration,
		ExpectedMetadataArtifactID: pgInt8(expectedMetadataArtifactID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("finalize scanner entity apply: %w", err)
	}
	return true, nil
}

func LoadScannerEntityArtifactResult(ctx context.Context, db *pgxpool.Pool, artifactID int64) (sqlc.ScannerEntityArtifact, Result, error) {
	if db == nil {
		return sqlc.ScannerEntityArtifact{}, Result{}, fmt.Errorf("scanner entity artifact DB is nil")
	}
	artifact, err := sqlc.New(db).GetScannerEntityArtifact(ctx, artifactID)
	if err != nil {
		return artifact, Result{}, fmt.Errorf("load scanner entity artifact %d: %w", artifactID, err)
	}
	result, err := unmarshalResultArtifact(artifact.Stage, artifact.Data)
	if err != nil {
		return artifact, Result{}, err
	}
	return artifact, result, nil
}

// ValidateCurrentScannerEntityArtifact is the generation/lineage half of the
// apply commit guard. Manual review mutations invalidate entity artifacts in
// the same SQL statement as the decision, so an in-flight apply observes the
// change immediately before its writes or commit and rolls back.
func ValidateCurrentScannerEntityArtifact(ctx context.Context, db *pgxpool.Pool, entityID, artifactID int64, stage string) error {
	if db == nil {
		return fmt.Errorf("validate current scanner artifact: database is nil")
	}
	_, err := sqlc.New(db).GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: artifactID, Stage: stage,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return &ArtifactReplayError{Reason: "artifact generation or manual decision was superseded"}
	}
	if err != nil {
		return fmt.Errorf("validate current scanner artifact: %w", err)
	}
	return nil
}

// ValidateCurrentScannerEntityArtifactTx performs the same generation/lineage
// check through the caller's transaction. GetCurrentScannerEntityArtifact uses
// FOR UPDATE, so a successful check serializes manual review/rematch mutations
// until that transaction commits. Domain apply calls this only after its
// canonical identity writes to preserve identity -> scanner-entity lock order.
func ValidateCurrentScannerEntityArtifactTx(ctx context.Context, tx pgx.Tx, entityID, artifactID int64, stage string) error {
	if tx == nil {
		return fmt.Errorf("validate current scanner artifact: transaction is nil")
	}
	_, err := sqlc.New(tx).GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: artifactID, Stage: stage,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return &ArtifactReplayError{Reason: "artifact generation or manual decision was superseded"}
	}
	if err != nil {
		return fmt.Errorf("validate current scanner artifact in apply transaction: %w", err)
	}
	return nil
}

func LoadCurrentScannerEntityArtifactResult(ctx context.Context, db *pgxpool.Pool, entityID, artifactID int64, stage string) (sqlc.ScannerEntityArtifact, Result, bool, error) {
	if db == nil {
		return sqlc.ScannerEntityArtifact{}, Result{}, false, fmt.Errorf("scanner entity artifact DB is nil")
	}
	artifact, err := sqlc.New(db).GetCurrentScannerEntityArtifact(ctx, sqlc.GetCurrentScannerEntityArtifactParams{
		EntityID: entityID, ArtifactID: artifactID, Stage: stage,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.ScannerEntityArtifact{}, Result{}, false, nil
	}
	if err != nil {
		return artifact, Result{}, false, fmt.Errorf("load current scanner entity artifact %d: %w", artifactID, err)
	}
	result, err := unmarshalResultArtifact(artifact.Stage, artifact.Data)
	if err != nil {
		return artifact, Result{}, false, err
	}
	return artifact, result, true, nil
}

func MarkScannerEntityFailed(ctx context.Context, db *pgxpool.Pool, entityID, expectedArtifactID int64, status string, failure error) (bool, error) {
	if db == nil || entityID == 0 || expectedArtifactID == 0 || failure == nil {
		return false, nil
	}
	if status == "" {
		status = "error"
	}
	q := sqlc.New(db)
	artifact, err := q.GetScannerEntityArtifact(ctx, expectedArtifactID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load scanner artifact for failure: %w", err)
	}
	_, markErr := q.MarkScannerEntityFailed(ctx, sqlc.MarkScannerEntityFailedParams{
		Status:             status,
		ErrorMessage:       failure.Error(),
		EntityID:           entityID,
		PipelineGeneration: artifact.PipelineGeneration,
		ExpectedArtifactID: expectedArtifactID,
	})
	if errors.Is(markErr, pgx.ErrNoRows) {
		return false, nil
	}
	if markErr != nil {
		return false, fmt.Errorf("persist scanner entity failure: %w", markErr)
	}
	return true, nil
}

func scannerEntityDrafts(lib sqlc.Library, result Result) []scannerEntityDraft {
	providerByKey, _ := scanIdentityTargets(result)
	reviewByKey := scanIdentityReviewStatuses(result)
	acceptedByKey := scannerAcceptedSearchByKey(lib, result)
	out := []scannerEntityDraft{}
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		for _, match := range result.MovieMatches {
			out = append(out, scannerEntityDraft{
				IdentityKey: match.Key,
				Title:       match.Title,
				Year:        match.Year,
				ProviderID:  providerByKey[match.Key],
				Accepted:    acceptedByKey[match.Key],
				Status:      scannerSearchEntityStatus(match.Key, providerByKey, reviewByKey, acceptedByKey),
				Data:        match,
			})
		}
	case lib.MediaType == sqlc.MediaTypeBook:
		for _, plan := range result.BookPlans {
			out = append(out, scannerEntityDraft{
				IdentityKey: plan.Key,
				Title:       plan.Title,
				Year:        plan.Year,
				ProviderID:  providerByKey[plan.Key],
				Accepted:    acceptedByKey[plan.Key],
				Status:      scannerSearchEntityStatus(plan.Key, providerByKey, reviewByKey, acceptedByKey),
				Data:        plan,
			})
		}
	case lib.MediaType == sqlc.MediaTypeMusic:
		for _, artist := range result.MusicArtists {
			out = append(out, scannerEntityDraft{
				IdentityKey: artist.Key,
				Title:       artist.Artist,
				ProviderID:  providerByKey[artist.Key],
				Accepted:    acceptedByKey[artist.Key],
				Status:      scannerSearchEntityStatus(artist.Key, providerByKey, reviewByKey, acceptedByKey),
				Data:        artist,
			})
		}
	case mediatype.IsTVLike(lib.MediaType):
		for _, match := range result.TVMatches {
			out = append(out, scannerEntityDraft{
				IdentityKey: match.Key,
				Title:       match.Title,
				Year:        match.Year,
				ProviderID:  providerByKey[match.Key],
				Accepted:    acceptedByKey[match.Key],
				Status:      scannerSearchEntityStatus(match.Key, providerByKey, reviewByKey, acceptedByKey),
				Data:        match,
			})
		}
	}
	return out
}

func scannerAcceptedSearchByKey(lib sqlc.Library, result Result) map[string]bool {
	out := map[string]bool{}
	switch {
	case lib.MediaType == sqlc.MediaTypeMovie:
		for _, search := range result.MovieSearch {
			out[search.Key] = search.Accepted && search.ProviderID != ""
		}
	case lib.MediaType == sqlc.MediaTypeBook:
		for _, search := range result.BookSearch {
			out[search.Key] = search.Accepted && search.ProviderID != ""
		}
	case lib.MediaType == sqlc.MediaTypeMusic:
		for _, search := range result.MusicSearch {
			out[search.Key] = search.Accepted && search.ProviderID != ""
		}
	case mediatype.IsTVLike(lib.MediaType):
		for _, search := range result.TVSearch {
			out[search.Key] = search.Accepted && search.ProviderID != ""
		}
	}
	return out
}

func scannerSearchEntityStatus(key string, providerByKey map[string]string, reviewByKey map[string]string, acceptedByKey map[string]bool) string {
	if review := reviewByKey[key]; review == "rejected" || review == "ignored" {
		return review
	} else if review == "needs_review" {
		return "needs_review"
	}
	if acceptedByKey[key] && providerByKey[key] != "" {
		return "matched"
	}
	if providerByKey[key] == "" {
		return "unmatched"
	}
	return "needs_review"
}

func scannerFetchEntityStatus(result Result) (string, string) {
	for _, item := range result.MovieMetadata {
		if item.Error != "" {
			return "metadata_error", item.Error
		}
		if item.Detail != nil {
			return "fetched", ""
		}
	}
	for _, item := range result.TVMetadata {
		if item.Error != "" {
			return "metadata_error", item.Error
		}
		if item.Detail != nil {
			return "fetched", ""
		}
	}
	for _, item := range result.MusicMetadata {
		if item.Error != "" {
			return "metadata_error", item.Error
		}
		if item.Detail != nil {
			return "fetched", ""
		}
	}
	for _, item := range result.BookMetadata {
		if item.Error != "" {
			return "metadata_error", item.Error
		}
		if item.Detail != nil {
			return "fetched", ""
		}
	}
	return "metadata_missing", "metadata detail is missing"
}

func scannerApplyEntityStatus(result Result) (string, string) {
	if status, msg, ok := movieApplyEntityStatus(result.MovieApply); ok {
		return status, msg
	}
	if status, msg, ok := tvApplyEntityStatus(result.TVApply); ok {
		return status, msg
	}
	if status, msg, ok := musicApplyEntityStatus(result.MusicApply); ok {
		return status, msg
	}
	if status, msg, ok := bookApplyEntityStatus(result.BookApply); ok {
		return status, msg
	}
	return "apply_missing", "apply produced no result"
}

func movieApplyEntityStatus(items []MovieApplyResult) (string, string, bool) {
	if len(items) == 0 {
		return "", "", false
	}
	return scannerApplyActionStatus(items[0].Action, items[0].Reason)
}

func tvApplyEntityStatus(items []TVApplyResult) (string, string, bool) {
	if len(items) == 0 {
		return "", "", false
	}
	return scannerApplyActionStatus(items[0].Action, items[0].Reason)
}

func musicApplyEntityStatus(items []MusicApplyResult) (string, string, bool) {
	if len(items) == 0 {
		return "", "", false
	}
	return scannerApplyActionStatus(items[0].Action, items[0].Reason)
}

func bookApplyEntityStatus(items []BookApplyResult) (string, string, bool) {
	if len(items) == 0 {
		return "", "", false
	}
	return scannerApplyActionStatus(items[0].Action, items[0].Reason)
}

func scannerApplyActionStatus(action string, reason string) (string, string, bool) {
	if action == "blocked" || action == "failed" || action == "skipped" {
		return action, firstNonEmpty(reason, "apply did not complete"), true
	}
	return "applied", "", true
}

func pgtypeZeroInt8() pgtype.Int8 {
	return pgtype.Int8{}
}
