package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

// ScannerOverview is the cheap, always-loadable slice of scanner state: run
// status, aggregate tallies, and the (small) terminal pipeline failures. The
// heavy collections behind those tallies — identities, findings, candidates —
// are served by their own paginated endpoints; a music library with 100k+
// open findings must never travel as one response again.
type ScannerOverview struct {
	LatestRun        *ScannerRunView              `json:"latest_run,omitempty"`
	BucketCounts     ScannerBucketCounts          `json:"bucket_counts"`
	PipelineFailures []ScannerPipelineFailureView `json:"pipeline_failures"`
	IssueCounts      []ScannerIssueCount          `json:"issue_counts"`
	IssueTotal       int64                        `json:"issue_total"`
}

// ScannerIssueCount tallies orphan findings (no identity) by code so the UI
// can render filter chips without loading rows.
type ScannerIssueCount struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Count    int64  `json:"count"`
}

type ScannerBucketCounts struct {
	Total       int `json:"total"`
	Matched     int `json:"matched"`
	NeedsReview int `json:"needs_review"`
	Rejected    int `json:"rejected"`
	Unmatched   int `json:"unmatched"`
	Ignored     int `json:"ignored"`
}

type ScannerRunView struct {
	ID                   int64          `json:"id"`
	LibraryID            int64          `json:"library_id"`
	MediaType            string         `json:"media_type"`
	ScannerVersion       string         `json:"scanner_version"`
	Mode                 string         `json:"mode"`
	Status               string         `json:"status"`
	Summary              map[string]any `json:"summary"`
	ErrorMessage         string         `json:"error_message,omitempty"`
	PipelineFailureCount int            `json:"pipeline_failure_count,omitempty"`
	PipelineErrorMessage string         `json:"pipeline_error_message,omitempty"`
	StartedAt            *time.Time     `json:"started_at,omitempty"`
	FinishedAt           *time.Time     `json:"finished_at,omitempty"`
	CreatedAt            *time.Time     `json:"created_at,omitempty"`
}

type ScannerPipelineFailureView struct {
	ID           int64      `json:"id"`
	IdentityKey  string     `json:"identity_key"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	Stage        string     `json:"stage"`
	ErrorMessage string     `json:"error_message"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
}

type ScannerFindingView struct {
	ID            int64          `json:"id"`
	ScanRunID     *int64         `json:"scan_run_id,omitempty"`
	LibraryID     int64          `json:"library_id"`
	MediaType     string         `json:"media_type"`
	IdentityID    *int64         `json:"identity_id,omitempty"`
	MediaItemID   *int64         `json:"media_item_id,omitempty"`
	LibraryFileID *int64         `json:"library_file_id,omitempty"`
	Severity      string         `json:"severity"`
	Code          string         `json:"code"`
	RelPath       string         `json:"rel_path,omitempty"`
	Message       string         `json:"message"`
	Data          map[string]any `json:"data"`
	CreatedAt     *time.Time     `json:"created_at,omitempty"`
	IdentityKey   string         `json:"identity_key,omitempty"`
	IdentityTitle string         `json:"identity_title,omitempty"`
	IdentityYear  string         `json:"identity_year,omitempty"`
	MediaTitle    string         `json:"media_title,omitempty"`
}

type ScannerIdentityView struct {
	ID                  int64      `json:"id"`
	LibraryID           int64      `json:"library_id"`
	MediaType           string     `json:"media_type"`
	IdentityKey         string     `json:"identity_key"`
	Title               string     `json:"title"`
	Year                string     `json:"year,omitempty"`
	Confidence          float32    `json:"confidence"`
	Source              string     `json:"source"`
	ReviewStatus        string     `json:"review_status"`
	Bucket              string     `json:"bucket"`
	MetadataProviderID  string     `json:"metadata_provider_id,omitempty"`
	MediaItemID         *int64     `json:"media_item_id,omitempty"`
	SelectedProviderID  string     `json:"selected_provider_id,omitempty"`
	SelectedTitle       string     `json:"selected_title,omitempty"`
	SelectedYear        string     `json:"selected_year,omitempty"`
	SelectedScore       *float64   `json:"selected_score,omitempty"`
	CandidateCount      int64      `json:"candidate_count"`
	OpenFindingCount    int64      `json:"open_finding_count"`
	MainFindingCode     string     `json:"main_finding_code,omitempty"`
	MainFindingSeverity string     `json:"main_finding_severity,omitempty"`
	MainFindingMessage  string     `json:"main_finding_message,omitempty"`
	LastSeenScanRunID   *int64     `json:"last_seen_scan_run_id,omitempty"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

type ScannerCandidateView struct {
	ID              int64             `json:"id"`
	IdentityID      int64             `json:"identity_id"`
	ScanRunID       *int64            `json:"scan_run_id,omitempty"`
	ProviderName    string            `json:"provider_name"`
	ProviderID      string            `json:"provider_id"`
	ProviderKind    string            `json:"provider_kind"`
	Title           string            `json:"title"`
	Year            string            `json:"year,omitempty"`
	Author          string            `json:"author,omitempty"`
	Description     string            `json:"description,omitempty"`
	PosterURL       string            `json:"poster_url,omitempty"`
	HeyaSlug        string            `json:"heya_slug,omitempty"`
	Score           *float64          `json:"score,omitempty"`
	Rank            int32             `json:"rank"`
	Status          string            `json:"status"`
	RejectionReason string            `json:"rejection_reason,omitempty"`
	ExternalIDs     map[string]string `json:"external_ids,omitempty"`
	IdentityKey     string            `json:"identity_key"`
	IdentityTitle   string            `json:"identity_title"`
	IdentityYear    string            `json:"identity_year,omitempty"`
}

type ScannerCandidateDetailView struct {
	CandidateID      int64             `json:"candidate_id"`
	ProviderID       string            `json:"provider_id"`
	ProviderName     string            `json:"provider_name"`
	ProviderKind     string            `json:"provider_kind"`
	Title            string            `json:"title"`
	Year             string            `json:"year,omitempty"`
	Author           string            `json:"author,omitempty"`
	Description      string            `json:"description,omitempty"`
	PosterURL        string            `json:"poster_url,omitempty"`
	BackdropURL      string            `json:"backdrop_url,omitempty"`
	HeyaSlug         string            `json:"heya_slug,omitempty"`
	Status           string            `json:"status,omitempty"`
	Genres           []string          `json:"genres,omitempty"`
	ExternalIDs      map[string]string `json:"external_ids,omitempty"`
	RuntimeMinutes   int               `json:"runtime_minutes,omitempty"`
	NumberOfSeasons  int               `json:"number_of_seasons,omitempty"`
	NumberOfEpisodes int               `json:"number_of_episodes,omitempty"`
	FirstAirDate     string            `json:"first_air_date,omitempty"`
	LastAirDate      string            `json:"last_air_date,omitempty"`
	Networks         []string          `json:"networks,omitempty"`
	ISBN             string            `json:"isbn,omitempty"`
	PageCount        int               `json:"page_count,omitempty"`
	Publisher        string            `json:"publisher,omitempty"`
	PublishDate      string            `json:"publish_date,omitempty"`
	Language         string            `json:"language,omitempty"`
	Subjects         []string          `json:"subjects,omitempty"`
}

type ScannerBulkApproveResult struct {
	Approved int `json:"approved"`
}

type ScannerBulkEligibleResult struct {
	Eligible int64 `json:"eligible"`
}

func (a *App) GetLibraryScannerOverview(ctx context.Context, libraryID int64) (ScannerOverview, error) {
	q := sqlc.New(a.db)
	overview := ScannerOverview{
		PipelineFailures: []ScannerPipelineFailureView{},
		IssueCounts:      []ScannerIssueCount{},
	}

	latest, err := q.GetLatestScannerRunByLibrary(ctx, libraryID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return overview, err
	}
	if err == nil {
		latestView := scannerRunView(latest)
		overview.LatestRun = &latestView
	}

	failures, err := q.ListFailedScannerEntitiesByLibrary(ctx, libraryID)
	if err != nil {
		return overview, err
	}
	overview.PipelineFailures = make([]ScannerPipelineFailureView, 0, len(failures))
	for _, failure := range failures {
		overview.PipelineFailures = append(overview.PipelineFailures, scannerPipelineFailureView(failure))
	}
	if overview.LatestRun != nil && len(overview.PipelineFailures) > 0 {
		overview.LatestRun.PipelineFailureCount = len(overview.PipelineFailures)
		overview.LatestRun.PipelineErrorMessage = overview.PipelineFailures[0].ErrorMessage
	}

	buckets, err := q.CountScannerIdentityBucketsByLibrary(ctx, libraryID)
	if err != nil {
		return overview, err
	}
	overview.BucketCounts = ScannerBucketCounts{
		Total:       int(buckets.Total),
		Matched:     int(buckets.Matched),
		NeedsReview: int(buckets.NeedsReview),
		Rejected:    int(buckets.Rejected),
		Unmatched:   int(buckets.Unmatched),
		Ignored:     int(buckets.Ignored),
	}

	issues, err := q.CountScannerIssuesByLibrary(ctx, libraryID)
	if err != nil {
		return overview, err
	}
	for _, issue := range issues {
		overview.IssueCounts = append(overview.IssueCounts, ScannerIssueCount{
			Code:     issue.Code,
			Severity: issue.Severity,
			Count:    issue.IssueCount,
		})
		overview.IssueTotal += issue.IssueCount
	}

	return overview, nil
}

// scannerBuckets are the valid values for the identities-page bucket filter —
// the computed CASE values of ListScannerIdentitiesPageByLibrary.
var scannerBuckets = map[string]bool{
	"matched": true, "needs_review": true, "unmatched": true,
	"rejected": true, "ignored": true,
}

func (a *App) ListScannerIdentitiesPage(ctx context.Context, libraryID int64, bucket, search string, limit, offset int32) ([]ScannerIdentityView, error) {
	if bucket != "" && !scannerBuckets[bucket] {
		return []ScannerIdentityView{}, nil
	}
	q := sqlc.New(a.db)
	rows, err := q.ListScannerIdentitiesPageByLibrary(ctx, sqlc.ListScannerIdentitiesPageByLibraryParams{
		LibraryID:  libraryID,
		Bucket:     bucket,
		Search:     strings.TrimSpace(search),
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, err
	}
	views := make([]ScannerIdentityView, 0, len(rows))
	for _, row := range rows {
		views = append(views, scannerIdentityView(row))
	}
	return views, nil
}

func (a *App) GetScannerIdentity(ctx context.Context, libraryID, identityID int64) (ScannerIdentityView, error) {
	return getScannerIdentityView(ctx, sqlc.New(a.db), libraryID, identityID)
}

func (a *App) ListScannerIdentityFindings(ctx context.Context, libraryID, identityID int64, limit, offset int32) ([]ScannerFindingView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListScannerFindingsByIdentity(ctx, sqlc.ListScannerFindingsByIdentityParams{
		LibraryID:  libraryID,
		IdentityID: pgtype.Int8{Int64: identityID, Valid: true},
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, err
	}
	views := make([]ScannerFindingView, 0, len(rows))
	for _, row := range rows {
		views = append(views, ScannerFindingView{
			ID:            row.ID,
			ScanRunID:     int8Ptr(row.ScanRunID),
			LibraryID:     row.LibraryID,
			MediaType:     string(row.MediaType),
			IdentityID:    int8Ptr(row.IdentityID),
			MediaItemID:   int8Ptr(row.MediaItemID),
			LibraryFileID: int8Ptr(row.LibraryFileID),
			Severity:      row.Severity,
			Code:          row.Code,
			RelPath:       secrettext.Redact(row.RelPath),
			Message:       secrettext.Redact(row.Message),
			CreatedAt:     timePtr(row.CreatedAt),
		})
	}
	return views, nil
}

func (a *App) ListScannerIssuesPage(ctx context.Context, libraryID int64, code string, limit, offset int32) ([]ScannerFindingView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListScannerIssuesPageByLibrary(ctx, sqlc.ListScannerIssuesPageByLibraryParams{
		LibraryID:  libraryID,
		Code:       strings.TrimSpace(code),
		PageLimit:  limit,
		PageOffset: offset,
	})
	if err != nil {
		return nil, err
	}
	views := make([]ScannerFindingView, 0, len(rows))
	for _, row := range rows {
		views = append(views, ScannerFindingView{
			ID:        row.ID,
			ScanRunID: int8Ptr(row.ScanRunID),
			LibraryID: row.LibraryID,
			MediaType: string(row.MediaType),
			Severity:  row.Severity,
			Code:      row.Code,
			RelPath:   secrettext.Redact(row.RelPath),
			Message:   secrettext.Redact(row.Message),
			CreatedAt: timePtr(row.CreatedAt),
		})
	}
	return views, nil
}

func (a *App) ListScannerIdentityCandidates(ctx context.Context, libraryID, identityID int64) ([]ScannerCandidateView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListScannerCandidatesByIdentity(ctx, sqlc.ListScannerCandidatesByIdentityParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
	})
	if err != nil {
		return nil, err
	}
	views := make([]ScannerCandidateView, 0, len(rows))
	for _, row := range rows {
		views = append(views, scannerCandidateView(row))
	}
	return views, nil
}

func (a *App) CountScannerBulkApproveEligible(ctx context.Context, libraryID int64, minConfidence float64) (int64, error) {
	q := sqlc.New(a.db)
	return q.CountScannerBulkApproveEligible(ctx, sqlc.CountScannerBulkApproveEligibleParams{
		LibraryID:     libraryID,
		MinConfidence: scannerPgNumericFromFloat64(minConfidence),
	})
}

var (
	ErrScannerReviewTargetNotFound = errors.New("scanner review target not found")
	// Terminal reject/ignore cannot safely detach a media item once scanner
	// apply has bound files to it: legacy file links do not retain enough
	// per-identity provenance to distinguish shared canonical items. Require a
	// rematch instead, which reassigns files through the normal apply pipeline.
	ErrScannerReviewIdentityApplied = errors.New("scanner identity is already applied; rematch it instead of rejecting or ignoring it")
)

func (a *App) GetScannerCandidateDetail(ctx context.Context, libraryID, identityID, candidateID int64) (ScannerCandidateDetailView, error) {
	q := sqlc.New(a.db)
	candidate, err := q.GetScannerCandidateForDetail(ctx, sqlc.GetScannerCandidateForDetailParams{
		LibraryID:   libraryID,
		IdentityID:  identityID,
		CandidateID: candidateID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerCandidateDetailView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerCandidateDetailView{}, err
	}
	if candidate.ProviderID == "" {
		return ScannerCandidateDetailView{}, fmt.Errorf("scanner candidate has no provider id")
	}
	detail, err := a.heya.GetDetail(ctx, candidate.ProviderID, nil)
	if err != nil {
		return ScannerCandidateDetailView{}, err
	}
	return scannerCandidateDetailView(candidate, detail), nil
}

func (a *App) ApproveScannerCandidate(ctx context.Context, libraryID, identityID, candidateID int64) (ScannerIdentityView, error) {
	q := sqlc.New(a.db)
	_, err := q.ApproveScannerCandidate(ctx, sqlc.ApproveScannerCandidateParams{
		LibraryID:   libraryID,
		IdentityID:  identityID,
		CandidateID: candidateID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerIdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerIdentityView{}, err
	}
	row, view, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	if err := a.enqueueScannerReviewApply(ctx, q, row); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int64("identity_id", identityID).Msg("scanner review approval: enqueue apply failed")
	}
	return view, nil
}

func (a *App) BulkApproveSingleScannerCandidates(ctx context.Context, libraryID int64, minConfidence float64) (ScannerBulkApproveResult, error) {
	q := sqlc.New(a.db)
	ids, err := q.BulkApproveSingleScannerCandidates(ctx, sqlc.BulkApproveSingleScannerCandidatesParams{
		LibraryID:     libraryID,
		MinConfidence: scannerPgNumericFromFloat64(minConfidence),
	})
	if err != nil {
		return ScannerBulkApproveResult{}, err
	}
	if len(ids) > 0 && a.river != nil {
		args := worker.ProcessLibraryScanArgs{LibraryID: libraryID, Force: true}
		if err := worker.EnqueueProcessLibraryScan(ctx, a.river, a.db, args, worker.PriorityMatch, ""); err != nil {
			log.Warn().Err(err).Int64("library_id", libraryID).Int("approved", len(ids)).Msg("scanner bulk approval: enqueue apply failed")
		}
	}
	return ScannerBulkApproveResult{Approved: len(ids)}, nil
}

func scannerCandidateDetailView(candidate sqlc.GetScannerCandidateForDetailRow, detail *metadata.MediaDetail) ScannerCandidateDetailView {
	out := ScannerCandidateDetailView{
		CandidateID:  candidate.ID,
		ProviderID:   candidate.ProviderID,
		ProviderName: candidate.ProviderName,
		ProviderKind: candidate.ProviderKind,
		Title:        candidate.Title,
		Year:         candidate.Year,
		Author:       stringFromJSONMap(jsonMap(candidate.RawData), "author"),
		ExternalIDs:  jsonStringMap(candidate.ExternalIds),
	}
	if detail == nil {
		return out
	}
	out.Title = scannerFirstNonEmpty(detail.Title, out.Title)
	out.Year = scannerFirstNonEmpty(detail.Year, out.Year)
	out.Author = scannerFirstNonEmpty(detail.AuthorName, out.Author)
	out.Description = detail.Description
	out.PosterURL = detail.PosterURL
	out.BackdropURL = detail.BackdropURL
	out.HeyaSlug = detail.HeyaSlug
	out.Status = detail.Status
	out.Genres = detail.Genres
	out.ExternalIDs = detail.ExternalIDs
	out.RuntimeMinutes = detail.RuntimeMinutes
	out.NumberOfSeasons = detail.NumberOfSeasons
	out.NumberOfEpisodes = detail.NumberOfEpisodes
	out.FirstAirDate = detail.FirstAirDate
	out.LastAirDate = detail.LastAirDate
	out.Networks = scannerNetworkNames(detail.Networks)
	out.ISBN = detail.ISBN
	out.PageCount = detail.PageCount
	out.Publisher = detail.Publisher
	out.PublishDate = detail.PublishDate
	out.Language = detail.Language
	out.Subjects = detail.Subjects
	return out
}

func scannerNetworkNames(networks []metadata.NetworkDetail) []string {
	out := make([]string, 0, len(networks))
	for _, network := range networks {
		if network.Name != "" {
			out = append(out, network.Name)
		}
	}
	return out
}

func scannerFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// SearchScannerIdentity runs a live provider search on behalf of a scanner
// identity — the "fix match" escape hatch for when the automated search never
// surfaced the right candidate. Accepts the same inputs as the metadata
// editor's identify search: free text + year, or a pasted provider URL /
// shortcode (TMDB, IMDb, TVDB, heya.media).
func (a *App) SearchScannerIdentity(ctx context.Context, libraryID, identityID int64, query, year string) (IdentifySearchResult, error) {
	q := sqlc.New(a.db)
	row, _, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return IdentifySearchResult{}, err
	}
	lib, err := q.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return IdentifySearchResult{}, err
	}
	settings := metadata.ParseSettings(lib.Settings)

	if query == "" {
		query = row.Title
	}
	if year == "" {
		year = row.Year
	}
	kind := scannerSearchKind(row.MediaType)

	if providerName, providerID, ok := parseIdentifyURL(query, kind); ok {
		results, err := a.searchIdentifyReference(ctx, kind, providerName, providerID, metadata.SearchQuery{
			Language: settings.PreferredLanguage, Country: settings.PreferredCountry,
		})
		if err != nil {
			return IdentifySearchResult{}, fmt.Errorf("look up external identity: %w", err)
		}
		return IdentifySearchResult{Results: results}, nil
	}

	searchQuery := metadata.SearchQuery{
		Title:    query,
		Year:     year,
		Language: settings.PreferredLanguage,
		Country:  settings.PreferredCountry,
	}
	if row.MediaType == sqlc.MediaTypeBook {
		var bookEvidence struct {
			Author      string            `json:"author"`
			Format      string            `json:"format"`
			ExternalIDs map[string]string `json:"external_ids"`
		}
		if json.Unmarshal(row.RawIdentity, &bookEvidence) == nil {
			searchQuery.Author = strings.TrimSpace(bookEvidence.Author)
			searchQuery.Format = strings.TrimSpace(bookEvidence.Format)
			if len(bookEvidence.ExternalIDs) > 0 {
				searchQuery.Identifiers = make(map[string]string, len(bookEvidence.ExternalIDs))
				for key, value := range bookEvidence.ExternalIDs {
					if value = strings.TrimSpace(value); value != "" {
						searchQuery.Identifiers[key] = value
					}
				}
			}
			for _, key := range []string{"isbn", "isbn13", "isbn_13", "isbn10", "isbn_10"} {
				if value := strings.TrimSpace(bookEvidence.ExternalIDs[key]); value != "" {
					searchQuery.ISBN = value
					break
				}
			}
		}
	}
	results, err := a.heya.Search(ctx, kind, searchQuery)
	if err != nil {
		log.Debug().Err(err).Msg("scanner identity search failed")
		results = nil
	}
	return IdentifySearchResult{Results: results}, nil
}

type AssignScannerIdentityReq struct {
	ProviderName string            `json:"provider_name,omitempty"`
	ProviderID   string            `json:"provider_id"`
	Title        string            `json:"title,omitempty"`
	Year         string            `json:"year,omitempty"`
	Description  string            `json:"description,omitempty"`
	PosterURL    string            `json:"poster_url,omitempty"`
	HeyaSlug     string            `json:"heya_slug,omitempty"`
	Confidence   float64           `json:"confidence,omitempty"`
	ExternalIDs  map[string]string `json:"external_ids,omitempty"`
}

// AssignScannerIdentityProvider pins an arbitrary provider result onto a
// scanner identity. The result is upserted as a match candidate row (so the
// review UI can render it like any scanner-found candidate) and then rides
// the normal approve flow: demote other candidates, resolve findings, and
// enqueue the scoped forced re-scan whose overlay materializes the decision.
func (a *App) AssignScannerIdentityProvider(ctx context.Context, libraryID, identityID int64, req AssignScannerIdentityReq) (ScannerIdentityView, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return ScannerIdentityView{}, fmt.Errorf("provider_id is required")
	}
	q := sqlc.New(a.db)
	row, _, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}

	providerName := scannerFirstNonEmpty(strings.TrimSpace(req.ProviderName), "heya")
	title := scannerFirstNonEmpty(strings.TrimSpace(req.Title), row.Title)
	confidence := req.Confidence
	if confidence <= 0 || confidence > 1 {
		confidence = 1
	}

	// raw_data mirrors the scanner's persisted *SearchCandidate JSON — the
	// review UI reads description/poster_url/heya_slug from it, and the music
	// candidate shape names its title field "artist".
	rawData := map[string]any{
		"provider_id": providerID,
		"provider":    providerName,
		"title":       title,
		"artist":      title,
		"confidence":  confidence,
		"manual":      true,
	}
	if req.Year != "" {
		rawData["year"] = req.Year
	}
	if req.Description != "" {
		rawData["description"] = req.Description
	}
	if req.PosterURL != "" {
		rawData["poster_url"] = req.PosterURL
	}
	if req.HeyaSlug != "" {
		rawData["heya_slug"] = req.HeyaSlug
	}
	if len(req.ExternalIDs) > 0 {
		rawData["external_ids"] = req.ExternalIDs
	}
	rawJSON, err := json.Marshal(rawData)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	externalJSON := []byte("{}")
	if len(req.ExternalIDs) > 0 {
		if externalJSON, err = json.Marshal(req.ExternalIDs); err != nil {
			return ScannerIdentityView{}, err
		}
	}

	candidate, err := q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID:      identityID,
		ScanRunID:       row.LastSeenScanRunID,
		ProviderName:    providerName,
		ProviderID:      providerID,
		ProviderKind:    scannerProviderKindFromID(providerID),
		Title:           title,
		Year:            req.Year,
		Score:           scannerPgNumericFromFloat64(confidence),
		Rank:            0, // sorts ahead of scanner-found candidates (rank >= 1)
		Status:          "candidate",
		RejectionReason: "",
		ExternalIds:     externalJSON,
		RawData:         rawJSON,
	})
	if err != nil {
		return ScannerIdentityView{}, err
	}
	return a.ApproveScannerCandidate(ctx, libraryID, identityID, candidate.ID)
}

func scannerSearchKind(mediaType sqlc.MediaType) metadata.MediaKind {
	switch mediaType {
	case sqlc.MediaTypeMovie:
		return metadata.KindMovie
	case sqlc.MediaTypeTv, sqlc.MediaTypeAnime:
		return metadata.KindTV
	case sqlc.MediaTypeMusic:
		return metadata.KindMusic
	case sqlc.MediaTypeBook:
		return metadata.KindBook
	}
	return metadata.KindMovie
}

func scannerProviderKindFromID(providerID string) string {
	return "heya"
}

func scannerPgNumericFromFloat64(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(strconv.FormatFloat(f, 'f', 3, 64)); err != nil {
		return pgtype.Numeric{Valid: true}
	}
	return n
}

func (a *App) RejectScannerIdentity(ctx context.Context, libraryID, identityID int64, reason string) (ScannerIdentityView, error) {
	q := sqlc.New(a.db)
	row, _, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	if row.MediaItemID.Valid {
		return ScannerIdentityView{}, ErrScannerReviewIdentityApplied
	}
	_, err = q.RejectScannerIdentity(ctx, sqlc.RejectScannerIdentityParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
		Reason:     scannerReviewReason(reason, "manual_rejected"),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if current, _, loadErr := getScannerIdentityRowAndView(ctx, q, libraryID, identityID); loadErr == nil && current.MediaItemID.Valid {
				return ScannerIdentityView{}, ErrScannerReviewIdentityApplied
			}
			return ScannerIdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerIdentityView{}, err
	}
	row, view, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	if err := a.enqueueScannerReviewReidentify(ctx, q, row); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int64("identity_id", identityID).Msg("scanner review rejection: enqueue re-identify failed")
	}
	return view, nil
}

func (a *App) IgnoreScannerIdentity(ctx context.Context, libraryID, identityID int64, reason string) (ScannerIdentityView, error) {
	q := sqlc.New(a.db)
	row, _, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	if row.MediaItemID.Valid {
		return ScannerIdentityView{}, ErrScannerReviewIdentityApplied
	}
	_, err = q.IgnoreScannerIdentity(ctx, sqlc.IgnoreScannerIdentityParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
		Reason:     scannerReviewReason(reason, "manual_ignored"),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if current, _, loadErr := getScannerIdentityRowAndView(ctx, q, libraryID, identityID); loadErr == nil && current.MediaItemID.Valid {
				return ScannerIdentityView{}, ErrScannerReviewIdentityApplied
			}
			return ScannerIdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerIdentityView{}, err
	}
	row, view, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	if err := a.enqueueScannerReviewReidentify(ctx, q, row); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int64("identity_id", identityID).Msg("scanner review ignore: enqueue re-identify failed")
	}
	return view, nil
}

func (a *App) ResetScannerIdentityReview(ctx context.Context, libraryID, identityID int64) (ScannerIdentityView, error) {
	q := sqlc.New(a.db)
	_, err := q.ResetScannerIdentityReview(ctx, sqlc.ResetScannerIdentityReviewParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerIdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerIdentityView{}, err
	}
	row, view, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	if err != nil {
		return ScannerIdentityView{}, err
	}
	if err := a.enqueueScannerReviewReidentify(ctx, q, row); err != nil {
		log.Warn().Err(err).Int64("library_id", libraryID).Int64("identity_id", identityID).Msg("scanner review reset: enqueue re-identify failed")
	}
	return view, nil
}

func (a *App) ListLibraryScannerRuns(ctx context.Context, libraryID int64, limit, offset int32) ([]ScannerRunView, error) {
	q := sqlc.New(a.db)
	runs, err := q.ListScannerRunsByLibrary(ctx, sqlc.ListScannerRunsByLibraryParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ScannerRunView, 0, len(runs))
	for _, run := range runs {
		out = append(out, scannerRunView(run))
	}
	if offset == 0 && len(out) > 0 {
		failures, err := q.ListFailedScannerEntitiesByLibrary(ctx, libraryID)
		if err != nil {
			return nil, err
		}
		if len(failures) > 0 {
			out[0].PipelineFailureCount = len(failures)
			out[0].PipelineErrorMessage = secrettext.Redact(failures[0].ErrorMessage)
		}
	}
	return out, nil
}

func scannerRunView(row sqlc.ScanRun) ScannerRunView {
	return ScannerRunView{
		ID:             row.ID,
		LibraryID:      row.LibraryID,
		MediaType:      string(row.MediaType),
		ScannerVersion: row.ScannerVersion,
		Mode:           row.Mode,
		Status:         row.Status,
		Summary:        secrettext.RedactMap(jsonMap(row.Summary)),
		ErrorMessage:   secrettext.Redact(row.ErrorMessage),
		StartedAt:      timePtr(row.StartedAt),
		FinishedAt:     timePtr(row.FinishedAt),
		CreatedAt:      timePtr(row.CreatedAt),
	}
}

func scannerPipelineFailureView(row sqlc.ScannerEntity) ScannerPipelineFailureView {
	stage := "scanner"
	switch row.Status {
	case "metadata_error":
		stage = "metadata fetch"
	case "apply_error":
		stage = "metadata apply"
	}
	return ScannerPipelineFailureView{
		ID:           row.ID,
		IdentityKey:  row.IdentityKey,
		Title:        row.Title,
		Status:       row.Status,
		Stage:        stage,
		ErrorMessage: secrettext.Redact(row.ErrorMessage),
		UpdatedAt:    timePtr(row.UpdatedAt),
	}
}

func scannerIdentityView(row sqlc.ListScannerIdentitiesPageByLibraryRow) ScannerIdentityView {
	return ScannerIdentityView{
		ID:                  row.ID,
		LibraryID:           row.LibraryID,
		MediaType:           string(row.MediaType),
		IdentityKey:         row.IdentityKey,
		Title:               row.Title,
		Year:                row.Year,
		Confidence:          row.Confidence,
		Source:              row.Source,
		ReviewStatus:        row.ReviewStatus,
		Bucket:              row.Bucket,
		MetadataProviderID:  row.MetadataProviderID,
		MediaItemID:         int8Ptr(row.MediaItemID),
		SelectedProviderID:  row.SelectedProviderID,
		SelectedTitle:       row.SelectedTitle,
		SelectedYear:        row.SelectedYear,
		SelectedScore:       numericPtr(row.SelectedScore),
		CandidateCount:      row.CandidateCount,
		OpenFindingCount:    row.OpenFindingCount,
		MainFindingCode:     row.MainFindingCode,
		MainFindingSeverity: row.MainFindingSeverity,
		MainFindingMessage:  secrettext.Redact(row.MainFindingMessage),
		LastSeenScanRunID:   int8Ptr(row.LastSeenScanRunID),
		UpdatedAt:           timePtr(row.UpdatedAt),
	}
}

func scannerIdentityViewFromGet(row sqlc.GetScannerIdentityForViewRow) ScannerIdentityView {
	bucket := scannerIdentityBucket(row.ReviewStatus, row.MediaItemID, row.SelectedProviderID, row.OpenFindingCount)
	return ScannerIdentityView{
		ID:                  row.ID,
		LibraryID:           row.LibraryID,
		MediaType:           string(row.MediaType),
		IdentityKey:         row.IdentityKey,
		Title:               row.Title,
		Year:                row.Year,
		Confidence:          row.Confidence,
		Source:              row.Source,
		ReviewStatus:        row.ReviewStatus,
		Bucket:              bucket,
		MetadataProviderID:  row.MetadataProviderID,
		MediaItemID:         int8Ptr(row.MediaItemID),
		SelectedProviderID:  row.SelectedProviderID,
		SelectedTitle:       row.SelectedTitle,
		SelectedYear:        row.SelectedYear,
		SelectedScore:       numericPtr(row.SelectedScore),
		CandidateCount:      row.CandidateCount,
		OpenFindingCount:    row.OpenFindingCount,
		MainFindingCode:     row.MainFindingCode,
		MainFindingSeverity: row.MainFindingSeverity,
		MainFindingMessage:  secrettext.Redact(row.MainFindingMessage),
		LastSeenScanRunID:   int8Ptr(row.LastSeenScanRunID),
		UpdatedAt:           timePtr(row.UpdatedAt),
	}
}

func getScannerIdentityView(ctx context.Context, q *sqlc.Queries, libraryID, identityID int64) (ScannerIdentityView, error) {
	_, view, err := getScannerIdentityRowAndView(ctx, q, libraryID, identityID)
	return view, err
}

func getScannerIdentityRowAndView(ctx context.Context, q *sqlc.Queries, libraryID, identityID int64) (sqlc.GetScannerIdentityForViewRow, ScannerIdentityView, error) {
	row, err := q.GetScannerIdentityForView(ctx, sqlc.GetScannerIdentityForViewParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return row, ScannerIdentityView{}, ErrScannerReviewTargetNotFound
		}
		return row, ScannerIdentityView{}, err
	}
	return row, scannerIdentityViewFromGet(row), nil
}

func (a *App) enqueueScannerReviewApply(ctx context.Context, q *sqlc.Queries, identity sqlc.GetScannerIdentityForViewRow) error {
	if a.river == nil {
		return nil
	}
	lib, err := q.GetLibraryByID(ctx, identity.LibraryID)
	if err != nil {
		return err
	}
	args := worker.ProcessLibraryScanArgs{
		LibraryID:  identity.LibraryID,
		ScopePaths: scannerReviewScopePaths(lib.Paths, identity.RawIdentity),
		Force:      true,
	}
	return worker.EnqueueProcessLibraryScan(ctx, a.river, a.db, args, worker.PriorityMatch, "")
}

func (a *App) enqueueScannerReviewReidentify(ctx context.Context, q *sqlc.Queries, identity sqlc.GetScannerIdentityForViewRow) error {
	if a.river == nil {
		return nil
	}
	lib, err := q.GetLibraryByID(ctx, identity.LibraryID)
	if err != nil {
		return err
	}
	args := worker.ProcessLibraryScanArgs{
		LibraryID:  identity.LibraryID,
		ScopePaths: scannerReviewScopePaths(lib.Paths, identity.RawIdentity),
		Force:      true,
	}
	return worker.EnqueueProcessLibraryScan(ctx, a.river, a.db, args, worker.PriorityMatch, "")
}

func scannerReviewScopePaths(libraryRoots []string, rawIdentity []byte) []string {
	files := scannerReviewIdentityFiles(rawIdentity)
	commonRelDir := scannerCommonRelDir(files)
	if commonRelDir == "" {
		return nil
	}
	scopes := make([]string, 0, len(libraryRoots))
	seen := map[string]bool{}
	for _, root := range libraryRoots {
		scope := scannerJoinScope(root, commonRelDir)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func scannerReviewIdentityFiles(rawIdentity []byte) []string {
	if len(rawIdentity) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(rawIdentity, &value); err != nil {
		return nil
	}
	files := map[string]bool{}
	collectScannerReviewIdentityFiles(value, "", files)
	out := make([]string, 0, len(files))
	for file := range files {
		out = append(out, file)
	}
	sort.Strings(out)
	return out
}

func collectScannerReviewIdentityFiles(value any, key string, files map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for childKey, childValue := range v {
			collectScannerReviewIdentityFiles(childValue, childKey, files)
		}
	case []any:
		if key == "files" {
			for _, item := range v {
				if file, ok := item.(string); ok {
					addScannerReviewIdentityFile(files, file)
				}
			}
			return
		}
		for _, item := range v {
			collectScannerReviewIdentityFiles(item, key, files)
		}
	case string:
		switch key {
		case "rel_path", "relPath", "path":
			addScannerReviewIdentityFile(files, v)
		}
	}
}

func addScannerReviewIdentityFile(files map[string]bool, file string) {
	file = strings.TrimSpace(file)
	if file == "" {
		return
	}
	files[file] = true
}

func scannerCommonRelDir(files []string) string {
	if len(files) == 0 {
		return ""
	}
	common := scannerRelDir(files[0])
	for _, file := range files[1:] {
		common = scannerCommonPathPrefix(common, scannerRelDir(file))
		if common == "" {
			return ""
		}
	}
	return common
}

func scannerRelDir(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Clean(file))
	if dir == "." {
		return "."
	}
	return dir
}

func scannerCommonPathPrefix(a, b string) string {
	if a == "" || b == "" {
		return ""
	}
	if a == "." || b == "." {
		if a == b {
			return "."
		}
		return ""
	}
	aParts := strings.Split(filepath.ToSlash(filepath.Clean(a)), "/")
	bParts := strings.Split(filepath.ToSlash(filepath.Clean(b)), "/")
	n := len(aParts)
	if len(bParts) < n {
		n = len(bParts)
	}
	var out []string
	for i := 0; i < n; i++ {
		if aParts[i] != bParts[i] {
			break
		}
		out = append(out, aParts[i])
	}
	if len(out) == 0 {
		return ""
	}
	return filepath.FromSlash(strings.Join(out, "/"))
}

func scannerJoinScope(root, relDir string) string {
	root = strings.TrimSpace(root)
	relDir = strings.TrimSpace(relDir)
	if root == "" || relDir == "" {
		return ""
	}
	if relDir == "." {
		return strings.TrimRight(root, "/")
	}
	if filepath.IsAbs(relDir) {
		return relDir
	}
	return filepath.Join(root, relDir)
}

func scannerIdentityBucket(reviewStatus string, mediaItemID pgtype.Int8, selectedProviderID string, openFindingCount int64) string {
	switch reviewStatus {
	case "rejected":
		return "rejected"
	case "ignored":
		return "ignored"
	case "needs_review", "review", "suspicious":
		return "needs_review"
	}
	if openFindingCount > 0 {
		return "needs_review"
	}
	if mediaItemID.Valid {
		return "matched"
	}
	return "unmatched"
}

func scannerReviewReason(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}

func scannerCandidateView(row sqlc.ListScannerCandidatesByIdentityRow) ScannerCandidateView {
	raw := jsonMap(row.RawData)
	return ScannerCandidateView{
		ID:              row.ID,
		IdentityID:      row.IdentityID,
		ScanRunID:       int8Ptr(row.ScanRunID),
		ProviderName:    row.ProviderName,
		ProviderID:      row.ProviderID,
		ProviderKind:    row.ProviderKind,
		Title:           row.Title,
		Year:            row.Year,
		Author:          stringFromJSONMap(raw, "author"),
		Description:     stringFromJSONMap(raw, "description"),
		PosterURL:       stringFromJSONMap(raw, "poster_url"),
		HeyaSlug:        stringFromJSONMap(raw, "heya_slug"),
		Score:           numericPtr(row.Score),
		Rank:            row.Rank,
		Status:          row.Status,
		RejectionReason: row.RejectionReason,
		ExternalIDs:     jsonStringMap(row.ExternalIds),
		IdentityKey:     row.IdentityKey,
		IdentityTitle:   row.IdentityTitle,
		IdentityYear:    row.IdentityYear,
	}
}

func stringFromJSONMap(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}

func jsonMap(data []byte) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func jsonStringMap(data []byte) map[string]string {
	if len(data) == 0 {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func int8Ptr(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func numericPtr(value pgtype.Numeric) *float64 {
	if !value.Valid {
		return nil
	}
	fv, err := value.Float64Value()
	if err != nil || !fv.Valid {
		return nil
	}
	v := fv.Float64
	return &v
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	v := value.Time
	return &v
}
