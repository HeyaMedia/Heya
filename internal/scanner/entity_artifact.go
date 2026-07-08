package scanner

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediatype"
)

const (
	scanArtifactKindApply = "apply_result"
)

type ScannerEntityRef struct {
	Entity       sqlc.ScannerEntity
	Artifact     sqlc.ScannerEntityArtifact
	Accepted     bool
	ProviderID   string
	IdentityKey  string
	ReviewStatus string
}

type scannerEntityDraft struct {
	IdentityKey string
	Title       string
	Year        string
	ProviderID  string
	Accepted    bool
	Status      string
	Data        any
}

func PersistScannerSearchEntities(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, result Result, scanRunID int64) ([]ScannerEntityRef, error) {
	return persistScannerEntitiesForStage(ctx, db, lib, opts, result, scanRunID, scanArtifactKindSearch)
}

func PersistScannerFetchEntity(ctx context.Context, db *pgxpool.Pool, entityID int64, result Result, scanRunID int64) (sqlc.ScannerEntityArtifact, error) {
	if db == nil {
		return sqlc.ScannerEntityArtifact{}, nil
	}
	q := sqlc.New(db)
	data, err := marshalResultArtifact(scanArtifactKindFetch, Options{}, result)
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, err
	}
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID:      entityID,
		Stage:         scanArtifactKindFetch,
		SchemaVersion: scanArtifactSchemaV1,
		ScanRunID:     pgInt8(scanRunID),
		Data:          data,
	})
	if err != nil {
		return artifact, fmt.Errorf("persist scanner entity fetch artifact: %w", err)
	}
	status, msg := scannerFetchEntityStatus(result)
	if _, err := q.MarkScannerEntityFetched(ctx, sqlc.MarkScannerEntityFetchedParams{
		ID:                 entityID,
		Status:             status,
		FetchScanRunID:     pgInt8(scanRunID),
		MetadataArtifactID: pgInt8(artifact.ID),
		ErrorMessage:       msg,
	}); err != nil {
		return artifact, fmt.Errorf("mark scanner entity fetched: %w", err)
	}
	return artifact, nil
}

func PersistScannerApplyEntity(ctx context.Context, db *pgxpool.Pool, entityID int64, result Result, scanRunID int64) (sqlc.ScannerEntityArtifact, error) {
	if db == nil {
		return sqlc.ScannerEntityArtifact{}, nil
	}
	q := sqlc.New(db)
	data, err := marshalResultArtifact(scanArtifactKindApply, Options{}, result)
	if err != nil {
		return sqlc.ScannerEntityArtifact{}, err
	}
	artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
		EntityID:      entityID,
		Stage:         scanArtifactKindApply,
		SchemaVersion: scanArtifactSchemaV1,
		ScanRunID:     pgInt8(scanRunID),
		Data:          data,
	})
	if err != nil {
		return artifact, fmt.Errorf("persist scanner entity apply artifact: %w", err)
	}
	status, msg := scannerApplyEntityStatus(result)
	if _, err := q.MarkScannerEntityApplied(ctx, sqlc.MarkScannerEntityAppliedParams{
		ID:              entityID,
		Status:          status,
		ApplyArtifactID: pgInt8(artifact.ID),
		ErrorMessage:    msg,
	}); err != nil {
		return artifact, fmt.Errorf("mark scanner entity applied: %w", err)
	}
	return artifact, nil
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

func MarkScannerEntityFailed(ctx context.Context, db *pgxpool.Pool, entityID int64, status string, err error) {
	if db == nil || entityID == 0 || err == nil {
		return
	}
	if status == "" {
		status = "error"
	}
	_, _ = sqlc.New(db).MarkScannerEntityFailed(ctx, sqlc.MarkScannerEntityFailedParams{
		ID:           entityID,
		Status:       status,
		ErrorMessage: err.Error(),
	})
}

func persistScannerEntitiesForStage(ctx context.Context, db *pgxpool.Pool, lib sqlc.Library, opts Options, result Result, scanRunID int64, stage string) ([]ScannerEntityRef, error) {
	if db == nil {
		return nil, nil
	}
	q := sqlc.New(db)
	result = filterResultToScopes(result, opts.ScopePaths, nil)
	drafts := scannerEntityDrafts(lib, result)
	refs := make([]ScannerEntityRef, 0, len(drafts))
	for _, draft := range drafts {
		narrow := filterResultToIdentityKey(result, draft.IdentityKey)
		data, err := marshalResultArtifact(stage, opts, narrow)
		if err != nil {
			return refs, err
		}
		entity, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
			LibraryID:        lib.ID,
			MediaType:        lib.MediaType,
			ScopeKey:         scannerScopeKey(opts.ScopePaths),
			ScopePaths:       normalizedScopePaths(opts.ScopePaths),
			IdentityKey:      draft.IdentityKey,
			Title:            draft.Title,
			Year:             draft.Year,
			ProviderID:       draft.ProviderID,
			Status:           draft.Status,
			SearchScanRunID:  pgInt8(scanRunID),
			SearchArtifactID: pgtypeZeroInt8(),
			ErrorMessage:     "",
			Data:             mustJSONBytes(draft.Data),
		})
		if err != nil {
			return refs, fmt.Errorf("upsert scanner entity %s: %w", draft.IdentityKey, err)
		}
		artifact, err := q.CreateScannerEntityArtifact(ctx, sqlc.CreateScannerEntityArtifactParams{
			EntityID:      entity.ID,
			Stage:         stage,
			SchemaVersion: scanArtifactSchemaV1,
			ScanRunID:     pgInt8(scanRunID),
			Data:          data,
		})
		if err != nil {
			return refs, fmt.Errorf("persist scanner entity artifact %s: %w", draft.IdentityKey, err)
		}
		entity, err = q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
			LibraryID:        lib.ID,
			MediaType:        lib.MediaType,
			ScopeKey:         scannerScopeKey(opts.ScopePaths),
			ScopePaths:       normalizedScopePaths(opts.ScopePaths),
			IdentityKey:      draft.IdentityKey,
			Title:            draft.Title,
			Year:             draft.Year,
			ProviderID:       draft.ProviderID,
			Status:           draft.Status,
			SearchScanRunID:  pgInt8(scanRunID),
			SearchArtifactID: pgInt8(artifact.ID),
			ErrorMessage:     "",
			Data:             mustJSONBytes(draft.Data),
		})
		if err != nil {
			return refs, fmt.Errorf("attach scanner entity artifact %s: %w", draft.IdentityKey, err)
		}
		refs = append(refs, ScannerEntityRef{
			Entity:       entity,
			Artifact:     artifact,
			Accepted:     draft.Accepted,
			ProviderID:   draft.ProviderID,
			IdentityKey:  draft.IdentityKey,
			ReviewStatus: draft.Status,
		})
	}
	return refs, nil
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
