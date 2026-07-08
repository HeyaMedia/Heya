package scanner

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

const persistedMusicCandidateLimit = 20

func PersistScanResult(ctx context.Context, lib sqlc.Library, result Result, events []Event, opts Options, db *pgxpool.Pool, summary map[string]any) (int64, error) {
	if db == nil {
		return 0, nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin scan persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	run, err := q.CreateScanRun(ctx, sqlc.CreateScanRunParams{
		LibraryID:      lib.ID,
		MediaType:      lib.MediaType,
		ScannerVersion: "scanner",
		Mode:           scanRunMode(opts),
		Status:         "running",
		Summary:        mustJSONBytes(summary),
	})
	if err != nil {
		return 0, fmt.Errorf("create scan run: %w", err)
	}

	identityByKey, err := persistLocalMediaIdentities(ctx, q, lib, run.ID, result)
	if err != nil {
		return 0, err
	}
	if err := persistMetadataMatchCandidates(ctx, q, run.ID, result, identityByKey); err != nil {
		return 0, err
	}
	if opts.RemoteSearch {
		if err := persistResultArtifactTx(ctx, q, run.ID, scanArtifactKindSearch, opts, result); err != nil {
			return 0, err
		}
	}
	if opts.FetchPreview {
		if err := persistResultArtifactTx(ctx, q, run.ID, scanArtifactKindFetch, opts, result); err != nil {
			return 0, err
		}
	}

	findings := scanFindingDrafts(result, events)
	if err := q.ResolveOpenScanFindingsByLibrary(ctx, sqlc.ResolveOpenScanFindingsByLibraryParams{
		LibraryID: lib.ID,
		MediaType: lib.MediaType,
		Column3:   managedScanFindingCodes(opts),
	}); err != nil {
		return 0, fmt.Errorf("resolve previous scan findings: %w", err)
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
			return 0, fmt.Errorf("create scan finding %s: %w", finding.Code, err)
		}
	}

	if err := q.FinishScanRun(ctx, sqlc.FinishScanRunParams{
		ID:           run.ID,
		Status:       "complete",
		Summary:      mustJSONBytes(summary),
		ErrorMessage: "",
	}); err != nil {
		return 0, fmt.Errorf("finish scan run: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit scan persistence: %w", err)
	}
	return run.ID, nil
}

func persistResultArtifactTx(ctx context.Context, q *sqlc.Queries, scanRunID int64, kind string, opts Options, result Result) error {
	data, err := marshalResultArtifact(kind, opts, result)
	if err != nil {
		return err
	}
	if _, err := q.UpsertScanRunArtifact(ctx, sqlc.UpsertScanRunArtifactParams{
		ScanRunID:     scanRunID,
		Kind:          kind,
		ScopeKey:      scannerScopeKey(opts.ScopePaths),
		SchemaVersion: scanArtifactSchemaV1,
		Data:          data,
	}); err != nil {
		return fmt.Errorf("persist scanner %s artifact: %w", kind, err)
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
			Source:             "scanner",
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
			Source:             "scanner",
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

	for _, artist := range result.MusicArtists {
		identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
			LibraryID:          lib.ID,
			MediaType:          lib.MediaType,
			IdentityKey:        artist.Key,
			Title:              artist.Artist,
			Year:               "",
			Confidence:         float32(artist.Confidence),
			Source:             "scanner",
			ReviewStatus:       firstNonEmpty(reviewByKey[artist.Key], "accepted"),
			MetadataProviderID: providerByKey[artist.Key],
			MediaItemID:        pgInt8(mediaItemByKey[artist.Key]),
			FirstSeenScanRunID: pgInt8(scanRunID),
			LastSeenScanRunID:  pgInt8(scanRunID),
			RawIdentity:        mustJSONBytes(artist),
		})
		if err != nil {
			return out, fmt.Errorf("upsert music local identity %s: %w", artist.Key, err)
		}
		out[artist.Key] = identity
		if err := persistLocalIdentityExternalIDs(ctx, q, identity.ID, artist.ExternalIDs); err != nil {
			return out, err
		}
	}

	for _, plan := range result.BookPlans {
		identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
			LibraryID:          lib.ID,
			MediaType:          lib.MediaType,
			IdentityKey:        plan.Key,
			Title:              plan.Title,
			Year:               plan.Year,
			Confidence:         float32(plan.Confidence),
			Source:             "scanner",
			ReviewStatus:       firstNonEmpty(reviewByKey[plan.Key], "accepted"),
			MetadataProviderID: providerByKey[plan.Key],
			MediaItemID:        pgInt8(mediaItemByKey[plan.Key]),
			FirstSeenScanRunID: pgInt8(scanRunID),
			LastSeenScanRunID:  pgInt8(scanRunID),
			RawIdentity:        mustJSONBytes(plan),
		})
		if err != nil {
			return out, fmt.Errorf("upsert book local identity %s: %w", plan.Key, err)
		}
		out[plan.Key] = identity
		if err := persistLocalIdentityExternalIDs(ctx, q, identity.ID, plan.ExternalIDs); err != nil {
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
			Source:     "scanner",
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
	for _, search := range result.MusicSearch {
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
		for rank, candidate := range limitMusicCandidates(search.Candidates, persistedMusicCandidateLimit) {
			status := "candidate"
			if search.Accepted && candidate.ProviderID == search.ProviderID {
				status = "selected"
			} else if !search.Accepted {
				status = "review_candidate"
			}
			if err := upsertMusicCandidate(ctx, q, scanRunID, identity.ID, rank+1, status, search.Reason, candidate); err != nil {
				return err
			}
		}
	}
	for _, search := range result.BookSearch {
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
			if err := upsertBookCandidate(ctx, q, scanRunID, identity.ID, rank+1, status, search.Reason, candidate); err != nil {
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

func upsertMusicCandidate(ctx context.Context, q *sqlc.Queries, scanRunID, identityID int64, rank int, status, reason string, candidate MusicSearchCandidate) error {
	if candidate.ProviderID == "" {
		return nil
	}
	_, err := q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID:      identityID,
		ScanRunID:       pgInt8(scanRunID),
		ProviderName:    firstNonEmpty(candidate.Provider, "heya"),
		ProviderID:      candidate.ProviderID,
		ProviderKind:    providerKindFromID(candidate.ProviderID),
		Title:           candidate.Artist,
		Year:            "",
		Score:           pgNumericFromFloat64(candidate.Confidence),
		Rank:            int32(rank),
		Status:          status,
		RejectionReason: reason,
		ExternalIds:     mustJSONBytes(candidate.ExternalIDs),
		RawData:         mustJSONBytes(candidate),
	})
	if err != nil {
		return fmt.Errorf("upsert music metadata candidate %s: %w", candidate.ProviderID, err)
	}
	return nil
}

func upsertBookCandidate(ctx context.Context, q *sqlc.Queries, scanRunID, identityID int64, rank int, status, reason string, candidate BookSearchCandidate) error {
	if candidate.ProviderID == "" {
		return nil
	}
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
		return fmt.Errorf("upsert book metadata candidate %s: %w", candidate.ProviderID, err)
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

	for _, search := range result.MusicSearch {
		if search.ProviderID != "" {
			providerByKey[search.Key] = search.ProviderID
		}
	}
	for _, preview := range result.MusicMaterialize {
		if preview.ProviderID != "" {
			providerByKey[preview.Key] = preview.ProviderID
		}
		if preview.MediaItemID != 0 {
			mediaItemByKey[preview.Key] = preview.MediaItemID
		}
	}
	for _, applied := range result.MusicApply {
		if applied.ProviderID != "" {
			providerByKey[applied.Key] = applied.ProviderID
		}
		if applied.MediaItemID != 0 {
			mediaItemByKey[applied.Key] = applied.MediaItemID
		}
	}
	for _, search := range result.BookSearch {
		if search.ProviderID != "" {
			providerByKey[search.Key] = search.ProviderID
		}
	}
	for _, preview := range result.BookMaterialize {
		if preview.ProviderID != "" {
			providerByKey[preview.Key] = preview.ProviderID
		}
		if preview.MediaItemID != 0 {
			mediaItemByKey[preview.Key] = preview.MediaItemID
		}
	}
	for _, applied := range result.BookApply {
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
	for _, artist := range result.MusicArtists {
		if len(artist.Issues) > 0 || musicArtistHasAlbumIssues(artist) {
			out[artist.Key] = "needs_review"
		}
	}
	for _, track := range result.MusicTracks {
		if len(track.Issues) > 0 {
			out[musicArtistKey(track.Artist, track.ArtistDisambiguation)] = "needs_review"
		}
	}
	for _, search := range result.MusicSearch {
		if search.ManualDecision != "" {
			if search.ManualDecision == "accepted" {
				delete(out, search.Key)
			} else {
				out[search.Key] = search.ManualDecision
			}
			continue
		}
		if !search.Accepted || musicSearchSelectionLooksSuspicious(search) {
			out[search.Key] = "needs_review"
		}
	}
	for _, meta := range result.MusicMetadata {
		if meta.Error != "" || musicMetadataMappingNeedsReview(meta) {
			out[meta.Key] = "needs_review"
		}
	}
	for _, preview := range result.MusicMaterialize {
		if preview.Action == "blocked" || len(preview.Issues) > 0 {
			out[preview.Key] = "needs_review"
		}
	}
	for _, applied := range result.MusicApply {
		if applied.Skipped || applied.Error != "" {
			out[applied.Key] = "needs_review"
		}
	}
	for _, plan := range result.BookPlans {
		if len(plan.Issues) > 0 {
			out[plan.Key] = "needs_review"
		}
	}
	for _, search := range result.BookSearch {
		if search.ManualDecision != "" {
			if search.ManualDecision == "accepted" {
				delete(out, search.Key)
			} else {
				out[search.Key] = search.ManualDecision
			}
			continue
		}
		if !search.Accepted || bookSearchSelectionLooksSuspicious(search) {
			out[search.Key] = "needs_review"
		}
	}
	for _, meta := range result.BookMetadata {
		if meta.Error != "" || len(meta.Issues) > 0 {
			out[meta.Key] = "needs_review"
		}
	}
	for _, preview := range result.BookMaterialize {
		if preview.Action == "blocked" || len(preview.Issues) > 0 {
			out[preview.Key] = "needs_review"
		}
	}
	for _, applied := range result.BookApply {
		if applied.Skipped || applied.Error != "" {
			out[applied.Key] = "needs_review"
		}
	}
	return out
}

func scanFindingDrafts(result Result, events []Event) []scanFindingDraft {
	var out []scanFindingDraft
	manualDecisionByKey := scanManualDecisions(result)
	for _, ev := range events {
		switch ev.Event {
		case "movie.file.unplanned", "tv.file.unplanned", "anime.file.unplanned", "book.file.unplanned":
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

	for _, artist := range result.MusicArtists {
		for _, issue := range artist.Issues {
			out = append(out, scanFindingDraft{Code: "local_identity_issue", Severity: string(SeverityWarn), Key: artist.Key, Message: issue, Data: artist})
		}
	}
	for _, album := range result.MusicAlbums {
		for _, issue := range album.Issues {
			out = append(out, scanFindingDraft{Code: "music_album_issue", Severity: string(SeverityWarn), Key: musicArtistKey(album.Artist, album.ArtistDisambiguation), Message: musicAlbumIssueMessage(album, issue), Data: album})
		}
	}
	for _, track := range result.MusicTracks {
		for _, issue := range track.Issues {
			out = append(out, scanFindingDraft{Code: "music_track_issue", Severity: string(SeverityWarn), Key: musicArtistKey(track.Artist, track.ArtistDisambiguation), RelPath: track.RelPath, Message: musicTrackIssueMessage(track, issue), Data: track})
		}
	}
	for _, search := range result.MusicSearch {
		if search.ManualDecision != "" {
			continue
		}
		if search.Error != "" || search.Reason == "search_error" {
			out = append(out, scanFindingDraft{Code: "search_error", Severity: string(SeverityWarn), Key: search.Key, Message: firstNonEmpty(search.Error, search.Reason, "search error"), Data: search})
		} else if !search.Accepted {
			out = append(out, scanFindingDraft{Code: "search_rejected", Severity: string(SeverityWarn), Key: search.Key, Message: firstNonEmpty(search.Reason, "search rejected"), Data: search})
		} else if musicSearchSelectionLooksSuspicious(search) {
			out = append(out, scanFindingDraft{Code: "search_suspicious", Severity: string(SeverityWarn), Key: search.Key, Message: "selected music artist result needs review", Data: search})
		}
	}
	for _, meta := range result.MusicMetadata {
		if meta.Error != "" {
			out = append(out, scanFindingDraft{Code: "metadata_fetch_failed", Severity: string(SeverityWarn), Key: meta.Key, Message: meta.Error, Data: meta})
		}
		if musicMetadataMappingNeedsReview(meta) {
			out = append(out, scanFindingDraft{Code: "music_metadata_mapping", Severity: string(SeverityWarn), Key: meta.Key, Message: musicMetadataMappingMessage(meta), Data: meta})
		}
	}
	for _, preview := range result.MusicMaterialize {
		if preview.Action == "blocked" {
			out = append(out, scanFindingDraft{Code: "materialization_blocked", Severity: string(SeverityWarn), Key: preview.Key, Message: firstNonEmpty(preview.Reason, "materialization blocked"), Data: preview, MediaItemID: preview.MediaItemID})
		}
	}
	for _, applied := range result.MusicApply {
		if applied.Error != "" {
			out = append(out, scanFindingDraft{Code: "materialization_failed", Severity: "error", Key: applied.Key, Message: applied.Error, Data: applied, MediaItemID: applied.MediaItemID})
		} else if applied.Skipped {
			out = append(out, scanFindingDraft{Code: "materialization_skipped", Severity: string(SeverityWarn), Key: applied.Key, Message: firstNonEmpty(applied.Reason, "materialization skipped"), Data: applied, MediaItemID: applied.MediaItemID})
		}
	}
	for _, plan := range result.BookPlans {
		for _, issue := range plan.Issues {
			out = append(out, scanFindingDraft{Code: "local_identity_issue", Severity: string(SeverityWarn), Key: plan.Key, Message: issue, Data: plan})
		}
	}
	for _, search := range result.BookSearch {
		if search.ManualDecision != "" {
			continue
		}
		if !search.Accepted {
			out = append(out, scanFindingDraft{Code: "search_rejected", Severity: string(SeverityWarn), Key: search.Key, Message: firstNonEmpty(search.Reason, "search rejected"), Data: search})
		} else if bookSearchSelectionLooksSuspicious(search) {
			out = append(out, scanFindingDraft{Code: "search_suspicious", Severity: string(SeverityWarn), Key: search.Key, Message: "selected book result needs review", Data: search})
		}
	}
	for _, meta := range result.BookMetadata {
		if meta.Error != "" {
			out = append(out, scanFindingDraft{Code: "metadata_fetch_failed", Severity: string(SeverityWarn), Key: meta.Key, Message: meta.Error, Data: meta})
		}
		for _, issue := range meta.Issues {
			out = append(out, scanFindingDraft{Code: "book_metadata_mapping", Severity: string(SeverityWarn), Key: meta.Key, Message: issue, Data: meta})
		}
	}
	for _, preview := range result.BookMaterialize {
		if preview.Action == "blocked" {
			out = append(out, scanFindingDraft{Code: "materialization_blocked", Severity: string(SeverityWarn), Key: preview.Key, Message: firstNonEmpty(preview.Reason, "materialization blocked"), Data: preview, MediaItemID: preview.MediaItemID})
		}
	}
	for _, applied := range result.BookApply {
		if applied.Error != "" {
			out = append(out, scanFindingDraft{Code: "materialization_failed", Severity: "error", Key: applied.Key, Message: applied.Error, Data: applied, MediaItemID: applied.MediaItemID})
		} else if applied.Skipped {
			out = append(out, scanFindingDraft{Code: "materialization_skipped", Severity: string(SeverityWarn), Key: applied.Key, Message: firstNonEmpty(applied.Reason, "materialization skipped"), Data: applied, MediaItemID: applied.MediaItemID})
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
	for _, search := range result.MusicSearch {
		if search.ManualDecision != "" {
			out[search.Key] = search.ManualDecision
		}
	}
	for _, search := range result.BookSearch {
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
		"music_album_issue",
		"music_track_issue",
	}
	if opts.RemoteSearch || opts.FetchPreview || opts.MaterializePreview || opts.Apply {
		codes = append(codes, "search_rejected", "search_error", "search_suspicious", "title_only_identity")
	}
	if opts.MaterializePreview || opts.Apply {
		codes = append(codes, "metadata_fetch_failed", "music_metadata_mapping", "book_metadata_mapping", "materialization_blocked", "materialization_failed", "materialization_skipped")
	}
	return codes
}

func musicArtistHasAlbumIssues(artist MusicArtistPlan) bool {
	for _, album := range artist.Albums {
		if len(album.Issues) > 0 {
			return true
		}
		for _, track := range album.Tracks {
			if len(track.Issues) > 0 {
				return true
			}
		}
	}
	return false
}

func musicAlbumIssueMessage(album MusicAlbumPlan, issue string) string {
	return firstNonEmpty(album.Album, "album") + ": " + issue
}

func musicTrackIssueMessage(track MusicTrackPlan, issue string) string {
	title := firstNonEmpty(track.TrackTitle, track.RelPath, "track")
	return title + ": " + issue
}

func musicMetadataMappingMessage(meta MusicFetchPreview) string {
	if meta.LocalAlbums > 0 && meta.MappedAlbums < meta.LocalAlbums {
		return fmt.Sprintf("mapped %d/%d albums and %d/%d tracks", meta.MappedAlbums, meta.LocalAlbums, meta.MappedTracks, meta.LocalTracks)
	}
	if meta.LocalTracks > 0 && meta.MappedTracks < meta.LocalTracks {
		return fmt.Sprintf("mapped %d/%d tracks", meta.MappedTracks, meta.LocalTracks)
	}
	if len(meta.Issues) > 0 {
		return meta.Issues[0]
	}
	return "music metadata mapping needs review"
}

func pgNumericFromFloat64(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(f, 'f', 3, 64)); err != nil {
		return pgtype.Numeric{Valid: true}
	}
	return n
}
