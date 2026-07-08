package ingestv2

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type scanFindingDraft struct {
	Code        string
	Severity    string
	Key         string
	RelPath     string
	Message     string
	Data        any
	MediaItemID int64
	FileID      int64
}

func PersistScanResult(ctx context.Context, lib sqlc.Library, result Result, events []Event, opts Options, db *pgxpool.Pool, summary map[string]any) error {
	if db == nil {
		return nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin scan persistence: %w", err)
	}
	defer tx.Rollback(ctx)

	q := sqlc.New(tx)
	run, err := q.CreateScanRun(ctx, sqlc.CreateScanRunParams{
		LibraryID:      lib.ID,
		MediaType:      lib.MediaType,
		ScannerVersion: "v2",
		Mode:           scanRunMode(opts),
		Status:         "running",
		Summary:        mustJSONBytes(summary),
	})
	if err != nil {
		return fmt.Errorf("create scan run: %w", err)
	}

	identityByKey, err := persistLocalMediaIdentities(ctx, q, lib, run.ID, result)
	if err != nil {
		return err
	}
	if err := persistMetadataMatchCandidates(ctx, q, run.ID, result, identityByKey); err != nil {
		return err
	}

	findings := scanFindingDrafts(result, events)
	if err := q.ResolveOpenScanFindingsByLibrary(ctx, sqlc.ResolveOpenScanFindingsByLibraryParams{
		LibraryID: lib.ID,
		MediaType: lib.MediaType,
		Column3:   managedScanFindingCodes(opts),
	}); err != nil {
		return fmt.Errorf("resolve previous scan findings: %w", err)
	}
	for _, finding := range findings {
		identityID := int64(0)
		if finding.Key != "" {
			if identity, ok := identityByKey[finding.Key]; ok {
				identityID = identity.ID
			}
		}
		if _, err := q.CreateScanFinding(ctx, sqlc.CreateScanFindingParams{
			ScanRunID:     pgInt8(run.ID),
			LibraryID:     lib.ID,
			MediaType:     lib.MediaType,
			IdentityID:    pgInt8(identityID),
			MediaItemID:   pgInt8(finding.MediaItemID),
			LibraryFileID: pgInt8(finding.FileID),
			Severity:      firstNonEmpty(finding.Severity, string(SeverityWarn)),
			Code:          finding.Code,
			RelPath:       finding.RelPath,
			Message:       finding.Message,
			Data:          mustJSONBytes(finding.Data),
		}); err != nil {
			return fmt.Errorf("create scan finding %s: %w", finding.Code, err)
		}
	}

	if err := q.FinishScanRun(ctx, sqlc.FinishScanRunParams{
		ID:           run.ID,
		Status:       "complete",
		Summary:      mustJSONBytes(summary),
		ErrorMessage: "",
	}); err != nil {
		return fmt.Errorf("finish scan run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit scan persistence: %w", err)
	}
	return nil
}

func persistLocalMediaIdentities(ctx context.Context, q *sqlc.Queries, lib sqlc.Library, scanRunID int64, result Result) (map[string]sqlc.LocalMediaIdentity, error) {
	providerByKey, mediaItemByKey := scanIdentityTargets(result)
	reviewByKey := scanIdentityReviewStatuses(result)
	out := map[string]sqlc.LocalMediaIdentity{}

	for _, match := range result.MovieMatches {
		identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
			LibraryID:          lib.ID,
			MediaType:          lib.MediaType,
			IdentityKey:        match.Key,
			Title:              match.Title,
			Year:               match.Year,
			Confidence:         float32(match.Confidence),
			Source:             "scanner_v2",
			ReviewStatus:       firstNonEmpty(reviewByKey[match.Key], "accepted"),
			MetadataProviderID: providerByKey[match.Key],
			MediaItemID:        pgInt8(mediaItemByKey[match.Key]),
			FirstSeenScanRunID: pgInt8(scanRunID),
			LastSeenScanRunID:  pgInt8(scanRunID),
			RawIdentity:        mustJSONBytes(match),
		})
		if err != nil {
			return out, fmt.Errorf("upsert movie local identity %s: %w", match.Key, err)
		}
		out[match.Key] = identity
		if err := persistLocalIdentityExternalIDs(ctx, q, identity.ID, match.ExternalIDs); err != nil {
			return out, err
		}
	}

	for _, match := range result.TVMatches {
		identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
			LibraryID:          lib.ID,
			MediaType:          lib.MediaType,
			IdentityKey:        match.Key,
			Title:              match.Title,
			Year:               match.Year,
			Confidence:         float32(match.Confidence),
			Source:             "scanner_v2",
			ReviewStatus:       firstNonEmpty(reviewByKey[match.Key], "accepted"),
			MetadataProviderID: providerByKey[match.Key],
			MediaItemID:        pgInt8(mediaItemByKey[match.Key]),
			FirstSeenScanRunID: pgInt8(scanRunID),
			LastSeenScanRunID:  pgInt8(scanRunID),
			RawIdentity:        mustJSONBytes(match),
		})
		if err != nil {
			return out, fmt.Errorf("upsert TV local identity %s: %w", match.Key, err)
		}
		out[match.Key] = identity
		if err := persistLocalIdentityExternalIDs(ctx, q, identity.ID, match.ExternalIDs); err != nil {
			return out, err
		}
	}

	return out, nil
}

func persistLocalIdentityExternalIDs(ctx context.Context, q *sqlc.Queries, identityID int64, ids map[string]string) error {
	for provider, externalID := range ids {
		if provider == "" || externalID == "" {
			continue
		}
		if err := q.UpsertLocalMediaIdentityExternalID(ctx, sqlc.UpsertLocalMediaIdentityExternalIDParams{
			IdentityID: identityID,
			Provider:   provider,
			ExternalID: externalID,
			Source:     "scanner_v2",
		}); err != nil {
			return fmt.Errorf("upsert local identity external id %s:%s: %w", provider, externalID, err)
		}
	}
	return nil
}

func persistMetadataMatchCandidates(ctx context.Context, q *sqlc.Queries, scanRunID int64, result Result, identityByKey map[string]sqlc.LocalMediaIdentity) error {
	cleared := map[int64]bool{}
	for _, search := range result.MovieSearch {
		identity, ok := identityByKey[search.Key]
		if !ok {
			continue
		}
		if scannerDecisionPreservesCandidates(search.ManualDecision, len(search.Candidates)) {
			continue
		}
		if err := clearMetadataMatchCandidates(ctx, q, identity.ID, cleared); err != nil {
			return err
		}
		for rank, candidate := range search.Candidates {
			status := "candidate"
			if search.Accepted && candidate.ProviderID == search.ProviderID {
				status = "selected"
			} else if !search.Accepted {
				status = "review_candidate"
			}
			if err := upsertMovieCandidate(ctx, q, scanRunID, identity.ID, rank+1, status, search.Reason, candidate); err != nil {
				return err
			}
		}
	}
	for _, search := range result.TVSearch {
		identity, ok := identityByKey[search.Key]
		if !ok {
			continue
		}
		if scannerDecisionPreservesCandidates(search.ManualDecision, len(search.Candidates)) {
			continue
		}
		if err := clearMetadataMatchCandidates(ctx, q, identity.ID, cleared); err != nil {
			return err
		}
		for rank, candidate := range search.Candidates {
			status := "candidate"
			if search.Accepted && candidate.ProviderID == search.ProviderID {
				status = "selected"
			} else if !search.Accepted {
				status = "review_candidate"
			}
			if err := upsertTVCandidate(ctx, q, scanRunID, identity.ID, rank+1, status, search.Reason, candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func scannerDecisionPreservesCandidates(decision string, candidateCount int) bool {
	return candidateCount == 0 && (decision == "rejected" || decision == "ignored")
}

func clearMetadataMatchCandidates(ctx context.Context, q *sqlc.Queries, identityID int64, cleared map[int64]bool) error {
	if cleared[identityID] {
		return nil
	}
	if err := q.DeleteMetadataMatchCandidatesByIdentity(ctx, identityID); err != nil {
		return fmt.Errorf("clear metadata candidates for identity %d: %w", identityID, err)
	}
	cleared[identityID] = true
	return nil
}

func upsertMovieCandidate(ctx context.Context, q *sqlc.Queries, scanRunID, identityID int64, rank int, status, reason string, candidate MovieSearchCandidate) error {
	_, err := q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID:      identityID,
		ScanRunID:       pgInt8(scanRunID),
		ProviderName:    firstNonEmpty(candidate.Provider, "heya"),
		ProviderID:      candidate.ProviderID,
		ProviderKind:    providerKindFromID(candidate.ProviderID),
		Title:           candidate.Title,
		Year:            candidate.Year,
		Score:           pgNumericFromFloat64(candidate.Confidence),
		Rank:            int32(rank),
		Status:          status,
		RejectionReason: reason,
		ExternalIds:     mustJSONBytes(candidate.ExternalIDs),
		RawData:         mustJSONBytes(candidate),
	})
	if err != nil {
		return fmt.Errorf("upsert movie metadata candidate %s: %w", candidate.ProviderID, err)
	}
	return nil
}

func upsertTVCandidate(ctx context.Context, q *sqlc.Queries, scanRunID, identityID int64, rank int, status, reason string, candidate TVSearchCandidate) error {
	_, err := q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID:      identityID,
		ScanRunID:       pgInt8(scanRunID),
		ProviderName:    firstNonEmpty(candidate.Provider, "heya"),
		ProviderID:      candidate.ProviderID,
		ProviderKind:    providerKindFromID(candidate.ProviderID),
		Title:           candidate.Title,
		Year:            candidate.Year,
		Score:           pgNumericFromFloat64(candidate.Confidence),
		Rank:            int32(rank),
		Status:          status,
		RejectionReason: reason,
		ExternalIds:     mustJSONBytes(candidate.ExternalIDs),
		RawData:         mustJSONBytes(candidate),
	})
	if err != nil {
		return fmt.Errorf("upsert TV metadata candidate %s: %w", candidate.ProviderID, err)
	}
	return nil
}

func scanIdentityTargets(result Result) (map[string]string, map[string]int64) {
	providerByKey := map[string]string{}
	mediaItemByKey := map[string]int64{}
	for _, search := range result.MovieSearch {
		if search.ProviderID != "" {
			providerByKey[search.Key] = search.ProviderID
		}
	}
	for _, preview := range result.MovieMaterialize {
		if preview.ProviderID != "" {
			providerByKey[preview.Key] = preview.ProviderID
		}
		if preview.MediaItemID != 0 {
			mediaItemByKey[preview.Key] = preview.MediaItemID
		}
	}
	for _, applied := range result.MovieApply {
		if applied.ProviderID != "" {
			providerByKey[applied.Key] = applied.ProviderID
		}
		if applied.MediaItemID != 0 {
			mediaItemByKey[applied.Key] = applied.MediaItemID
		}
	}

	for _, search := range result.TVSearch {
		if search.ProviderID != "" {
			providerByKey[search.Key] = search.ProviderID
		}
	}
	for _, preview := range result.TVMaterialize {
		keys := preview.Keys
		if len(keys) == 0 && preview.Key != "" {
			keys = []string{preview.Key}
		}
		for _, key := range keys {
			if preview.ProviderID != "" {
				providerByKey[key] = preview.ProviderID
			}
			if preview.MediaItemID != 0 {
				mediaItemByKey[key] = preview.MediaItemID
			}
		}
	}
	for _, applied := range result.TVApply {
		if applied.ProviderID != "" {
			providerByKey[applied.Key] = applied.ProviderID
		}
		if applied.MediaItemID != 0 {
			mediaItemByKey[applied.Key] = applied.MediaItemID
		}
	}
	return providerByKey, mediaItemByKey
}

func scanIdentityReviewStatuses(result Result) map[string]string {
	out := map[string]string{}
	for _, match := range result.MovieMatches {
		if len(match.Issues) > 0 {
			out[match.Key] = "needs_review"
		}
	}
	for _, search := range result.MovieSearch {
		if search.ManualDecision != "" {
			if search.ManualDecision == "accepted" {
				delete(out, search.Key)
			} else {
				out[search.Key] = search.ManualDecision
			}
			continue
		}
		if !search.Accepted || searchSelectionLooksSuspicious(search) {
			out[search.Key] = "needs_review"
		}
	}
	for _, preview := range result.MovieMaterialize {
		if preview.Action == "blocked" {
			out[preview.Key] = "needs_review"
		}
	}
	for _, match := range result.TVMatches {
		if len(match.Issues) > 0 || match.KeyType == "title" {
			out[match.Key] = "needs_review"
		}
	}
	for _, search := range result.TVSearch {
		if search.ManualDecision != "" {
			if search.ManualDecision == "accepted" {
				delete(out, search.Key)
			} else {
				out[search.Key] = search.ManualDecision
			}
			continue
		}
		if !search.Accepted || tvSearchSelectionLooksSuspicious(search) {
			out[search.Key] = "needs_review"
		}
	}
	for _, preview := range result.TVMaterialize {
		if preview.Action == "blocked" {
			for _, key := range append([]string{preview.Key}, preview.Keys...) {
				if key != "" {
					out[key] = "needs_review"
				}
			}
		}
	}
	return out
}

func scanFindingDrafts(result Result, events []Event) []scanFindingDraft {
	var out []scanFindingDraft
	manualDecisionByKey := scanManualDecisions(result)
	for _, ev := range events {
		switch ev.Event {
		case "movie.file.unplanned", "tv.file.unplanned":
			out = append(out, scanFindingDraft{
				Code:     "unplanned_media",
				Severity: string(ev.Severity),
				RelPath:  ev.RelPath,
				Message:  firstNonEmpty(ev.Message, ev.Reason),
				Data:     ev,
			})
		case "nfo.parse_failed":
			out = append(out, scanFindingDraft{
				Code:     "nfo_parse_failed",
				Severity: string(ev.Severity),
				RelPath:  ev.RelPath,
				Message:  firstNonEmpty(ev.Message, "NFO parse failed"),
				Data:     ev,
			})
		case "plexmatch.parse_failed":
			out = append(out, scanFindingDraft{
				Code:     "plexmatch_parse_failed",
				Severity: string(ev.Severity),
				RelPath:  ev.RelPath,
				Message:  firstNonEmpty(ev.Message, ev.Reason, "plexmatch parse failed"),
				Data:     ev,
			})
		}
	}

	for _, match := range result.MovieMatches {
		for _, issue := range match.Issues {
			out = append(out, scanFindingDraft{Code: "local_identity_issue", Severity: string(SeverityWarn), Key: match.Key, Message: issue, Data: match})
		}
	}
	for _, search := range result.MovieSearch {
		if search.ManualDecision != "" {
			continue
		}
		if !search.Accepted {
			out = append(out, scanFindingDraft{Code: "search_rejected", Severity: string(SeverityWarn), Key: search.Key, Message: firstNonEmpty(search.Reason, "search rejected"), Data: search})
		} else if searchSelectionLooksSuspicious(search) {
			out = append(out, scanFindingDraft{Code: "search_suspicious", Severity: string(SeverityWarn), Key: search.Key, Message: "selected search result needs review", Data: search})
		}
	}
	for _, preview := range result.MovieMaterialize {
		if preview.Action == "blocked" {
			out = append(out, scanFindingDraft{Code: "materialization_blocked", Severity: string(SeverityWarn), Key: preview.Key, Message: firstNonEmpty(preview.Reason, "materialization blocked"), Data: preview, MediaItemID: preview.MediaItemID})
		}
	}

	for _, match := range result.TVMatches {
		if match.KeyType == "title" && manualDecisionByKey[match.Key] == "" {
			out = append(out, scanFindingDraft{Code: "title_only_identity", Severity: string(SeverityWarn), Key: match.Key, Message: "TV show identity is title-only", Data: match})
		}
		for _, issue := range match.Issues {
			out = append(out, scanFindingDraft{Code: "local_identity_issue", Severity: string(SeverityWarn), Key: match.Key, Message: issue, Data: match})
		}
	}
	for _, search := range result.TVSearch {
		if search.ManualDecision != "" {
			continue
		}
		if !search.Accepted {
			out = append(out, scanFindingDraft{Code: "search_rejected", Severity: string(SeverityWarn), Key: search.Key, Message: firstNonEmpty(search.Reason, "search rejected"), Data: search})
		} else if tvSearchSelectionLooksSuspicious(search) {
			out = append(out, scanFindingDraft{Code: "search_suspicious", Severity: string(SeverityWarn), Key: search.Key, Message: "selected search result needs review", Data: search})
		}
	}
	for _, preview := range result.TVMaterialize {
		if preview.Action == "blocked" {
			out = append(out, scanFindingDraft{Code: "materialization_blocked", Severity: string(SeverityWarn), Key: preview.Key, Message: firstNonEmpty(preview.Reason, "materialization blocked"), Data: preview, MediaItemID: preview.MediaItemID})
		}
	}
	return out
}

func scanManualDecisions(result Result) map[string]string {
	out := map[string]string{}
	for _, search := range result.MovieSearch {
		if search.ManualDecision != "" {
			out[search.Key] = search.ManualDecision
		}
	}
	for _, search := range result.TVSearch {
		if search.ManualDecision != "" {
			out[search.Key] = search.ManualDecision
		}
	}
	return out
}

func managedScanFindingCodes(opts Options) []string {
	codes := []string{
		"unplanned_media",
		"nfo_parse_failed",
		"plexmatch_parse_failed",
		"local_identity_issue",
	}
	if opts.RemoteSearch || opts.FetchPreview || opts.MaterializePreview || opts.Apply {
		codes = append(codes, "search_rejected", "search_suspicious", "title_only_identity")
	}
	if opts.MaterializePreview || opts.Apply {
		codes = append(codes, "materialization_blocked")
	}
	return codes
}

func pgNumericFromFloat64(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(f, 'f', 3, 64)); err != nil {
		return pgtype.Numeric{Valid: true}
	}
	return n
}
