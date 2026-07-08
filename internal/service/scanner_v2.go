package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type ScannerV2View struct {
	LatestRun    *ScannerV2RunView        `json:"latest_run,omitempty"`
	BucketCounts ScannerV2BucketCounts    `json:"bucket_counts"`
	OpenFindings []ScannerV2FindingView   `json:"open_findings"`
	Identities   []ScannerV2IdentityView  `json:"identities"`
	Candidates   []ScannerV2CandidateView `json:"candidates,omitempty"`
}

type ScannerV2BucketCounts struct {
	Total       int `json:"total"`
	Matched     int `json:"matched"`
	NeedsReview int `json:"needs_review"`
	Rejected    int `json:"rejected"`
	Unmatched   int `json:"unmatched"`
	Ignored     int `json:"ignored"`
}

type ScannerV2RunView struct {
	ID             int64          `json:"id"`
	LibraryID      int64          `json:"library_id"`
	MediaType      string         `json:"media_type"`
	ScannerVersion string         `json:"scanner_version"`
	Mode           string         `json:"mode"`
	Status         string         `json:"status"`
	Summary        map[string]any `json:"summary"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
	CreatedAt      *time.Time     `json:"created_at,omitempty"`
}

type ScannerV2FindingView struct {
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

type ScannerV2IdentityView struct {
	ID                 int64      `json:"id"`
	LibraryID          int64      `json:"library_id"`
	MediaType          string     `json:"media_type"`
	IdentityKey        string     `json:"identity_key"`
	Title              string     `json:"title"`
	Year               string     `json:"year,omitempty"`
	Confidence         float32    `json:"confidence"`
	Source             string     `json:"source"`
	ReviewStatus       string     `json:"review_status"`
	Bucket             string     `json:"bucket"`
	MetadataProviderID string     `json:"metadata_provider_id,omitempty"`
	MediaItemID        *int64     `json:"media_item_id,omitempty"`
	SelectedProviderID string     `json:"selected_provider_id,omitempty"`
	SelectedTitle      string     `json:"selected_title,omitempty"`
	SelectedYear       string     `json:"selected_year,omitempty"`
	SelectedScore      *float64   `json:"selected_score,omitempty"`
	CandidateCount     int64      `json:"candidate_count"`
	OpenFindingCount   int64      `json:"open_finding_count"`
	LastSeenScanRunID  *int64     `json:"last_seen_scan_run_id,omitempty"`
	UpdatedAt          *time.Time `json:"updated_at,omitempty"`
}

type ScannerV2CandidateView struct {
	ID              int64             `json:"id"`
	IdentityID      int64             `json:"identity_id"`
	ScanRunID       *int64            `json:"scan_run_id,omitempty"`
	ProviderName    string            `json:"provider_name"`
	ProviderID      string            `json:"provider_id"`
	ProviderKind    string            `json:"provider_kind"`
	Title           string            `json:"title"`
	Year            string            `json:"year,omitempty"`
	Score           *float64          `json:"score,omitempty"`
	Rank            int32             `json:"rank"`
	Status          string            `json:"status"`
	RejectionReason string            `json:"rejection_reason,omitempty"`
	ExternalIDs     map[string]string `json:"external_ids,omitempty"`
	IdentityKey     string            `json:"identity_key"`
	IdentityTitle   string            `json:"identity_title"`
	IdentityYear    string            `json:"identity_year,omitempty"`
}

func (a *App) GetLibraryScannerV2View(ctx context.Context, libraryID int64, includeCandidates bool) (ScannerV2View, error) {
	q := sqlc.New(a.db)
	view := ScannerV2View{
		OpenFindings: []ScannerV2FindingView{},
		Identities:   []ScannerV2IdentityView{},
	}

	latest, err := q.GetLatestScannerRunByLibrary(ctx, libraryID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return view, err
	}
	if err == nil {
		latestView := scannerRunView(latest)
		view.LatestRun = &latestView
	}

	findings, err := q.ListOpenScannerFindingsByLibrary(ctx, libraryID)
	if err != nil {
		return view, err
	}
	view.OpenFindings = make([]ScannerV2FindingView, 0, len(findings))
	for _, finding := range findings {
		view.OpenFindings = append(view.OpenFindings, scannerFindingView(finding))
	}

	identities, err := q.ListScannerIdentitiesByLibrary(ctx, libraryID)
	if err != nil {
		return view, err
	}
	view.Identities = make([]ScannerV2IdentityView, 0, len(identities))
	for _, identity := range identities {
		identityView := scannerIdentityView(identity)
		view.Identities = append(view.Identities, identityView)
		addScannerBucketCount(&view.BucketCounts, identityView.Bucket)
	}

	if includeCandidates {
		candidates, err := q.ListScannerCandidatesByLibrary(ctx, libraryID)
		if err != nil {
			return view, err
		}
		view.Candidates = make([]ScannerV2CandidateView, 0, len(candidates))
		for _, candidate := range candidates {
			view.Candidates = append(view.Candidates, scannerCandidateView(candidate))
		}
	}

	return view, nil
}

var ErrScannerReviewTargetNotFound = errors.New("scanner review target not found")

func (a *App) ApproveScannerV2Candidate(ctx context.Context, libraryID, identityID, candidateID int64) (ScannerV2IdentityView, error) {
	q := sqlc.New(a.db)
	_, err := q.ApproveScannerCandidate(ctx, sqlc.ApproveScannerCandidateParams{
		LibraryID:   libraryID,
		IdentityID:  identityID,
		CandidateID: candidateID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerV2IdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerV2IdentityView{}, err
	}
	return getScannerIdentityView(ctx, q, libraryID, identityID)
}

func (a *App) RejectScannerV2Identity(ctx context.Context, libraryID, identityID int64, reason string) (ScannerV2IdentityView, error) {
	q := sqlc.New(a.db)
	_, err := q.RejectScannerIdentity(ctx, sqlc.RejectScannerIdentityParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
		Reason:     scannerReviewReason(reason, "manual_rejected"),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerV2IdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerV2IdentityView{}, err
	}
	return getScannerIdentityView(ctx, q, libraryID, identityID)
}

func (a *App) IgnoreScannerV2Identity(ctx context.Context, libraryID, identityID int64, reason string) (ScannerV2IdentityView, error) {
	q := sqlc.New(a.db)
	_, err := q.IgnoreScannerIdentity(ctx, sqlc.IgnoreScannerIdentityParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
		Reason:     scannerReviewReason(reason, "manual_ignored"),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerV2IdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerV2IdentityView{}, err
	}
	return getScannerIdentityView(ctx, q, libraryID, identityID)
}

func (a *App) ResetScannerV2IdentityReview(ctx context.Context, libraryID, identityID int64) (ScannerV2IdentityView, error) {
	q := sqlc.New(a.db)
	_, err := q.ResetScannerIdentityReview(ctx, sqlc.ResetScannerIdentityReviewParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerV2IdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerV2IdentityView{}, err
	}
	return getScannerIdentityView(ctx, q, libraryID, identityID)
}

func (a *App) ListLibraryScannerV2Runs(ctx context.Context, libraryID int64, limit, offset int32) ([]ScannerV2RunView, error) {
	q := sqlc.New(a.db)
	runs, err := q.ListScannerRunsByLibrary(ctx, sqlc.ListScannerRunsByLibraryParams{
		LibraryID: libraryID,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ScannerV2RunView, 0, len(runs))
	for _, run := range runs {
		out = append(out, scannerRunView(run))
	}
	return out, nil
}

func scannerRunView(row sqlc.ScanRun) ScannerV2RunView {
	return ScannerV2RunView{
		ID:             row.ID,
		LibraryID:      row.LibraryID,
		MediaType:      string(row.MediaType),
		ScannerVersion: row.ScannerVersion,
		Mode:           row.Mode,
		Status:         row.Status,
		Summary:        jsonMap(row.Summary),
		ErrorMessage:   row.ErrorMessage,
		StartedAt:      timePtr(row.StartedAt),
		FinishedAt:     timePtr(row.FinishedAt),
		CreatedAt:      timePtr(row.CreatedAt),
	}
}

func scannerFindingView(row sqlc.ListOpenScannerFindingsByLibraryRow) ScannerV2FindingView {
	return ScannerV2FindingView{
		ID:            row.ID,
		ScanRunID:     int8Ptr(row.ScanRunID),
		LibraryID:     row.LibraryID,
		MediaType:     string(row.MediaType),
		IdentityID:    int8Ptr(row.IdentityID),
		MediaItemID:   int8Ptr(row.MediaItemID),
		LibraryFileID: int8Ptr(row.LibraryFileID),
		Severity:      row.Severity,
		Code:          row.Code,
		RelPath:       row.RelPath,
		Message:       row.Message,
		Data:          jsonMap(row.Data),
		CreatedAt:     timePtr(row.CreatedAt),
		IdentityKey:   textValue(row.IdentityKey),
		IdentityTitle: textValue(row.IdentityTitle),
		IdentityYear:  textValue(row.IdentityYear),
		MediaTitle:    textValue(row.MediaTitle),
	}
}

func scannerIdentityView(row sqlc.ListScannerIdentitiesByLibraryRow) ScannerV2IdentityView {
	bucket := scannerIdentityBucket(row.ReviewStatus, row.MediaItemID, row.SelectedProviderID, row.OpenFindingCount)
	return ScannerV2IdentityView{
		ID:                 row.ID,
		LibraryID:          row.LibraryID,
		MediaType:          string(row.MediaType),
		IdentityKey:        row.IdentityKey,
		Title:              row.Title,
		Year:               row.Year,
		Confidence:         row.Confidence,
		Source:             row.Source,
		ReviewStatus:       row.ReviewStatus,
		Bucket:             bucket,
		MetadataProviderID: row.MetadataProviderID,
		MediaItemID:        int8Ptr(row.MediaItemID),
		SelectedProviderID: row.SelectedProviderID,
		SelectedTitle:      row.SelectedTitle,
		SelectedYear:       row.SelectedYear,
		SelectedScore:      numericPtr(row.SelectedScore),
		CandidateCount:     row.CandidateCount,
		OpenFindingCount:   row.OpenFindingCount,
		LastSeenScanRunID:  int8Ptr(row.LastSeenScanRunID),
		UpdatedAt:          timePtr(row.UpdatedAt),
	}
}

func scannerIdentityViewFromGet(row sqlc.GetScannerIdentityForViewRow) ScannerV2IdentityView {
	bucket := scannerIdentityBucket(row.ReviewStatus, row.MediaItemID, row.SelectedProviderID, row.OpenFindingCount)
	return ScannerV2IdentityView{
		ID:                 row.ID,
		LibraryID:          row.LibraryID,
		MediaType:          string(row.MediaType),
		IdentityKey:        row.IdentityKey,
		Title:              row.Title,
		Year:               row.Year,
		Confidence:         row.Confidence,
		Source:             row.Source,
		ReviewStatus:       row.ReviewStatus,
		Bucket:             bucket,
		MetadataProviderID: row.MetadataProviderID,
		MediaItemID:        int8Ptr(row.MediaItemID),
		SelectedProviderID: row.SelectedProviderID,
		SelectedTitle:      row.SelectedTitle,
		SelectedYear:       row.SelectedYear,
		SelectedScore:      numericPtr(row.SelectedScore),
		CandidateCount:     row.CandidateCount,
		OpenFindingCount:   row.OpenFindingCount,
		LastSeenScanRunID:  int8Ptr(row.LastSeenScanRunID),
		UpdatedAt:          timePtr(row.UpdatedAt),
	}
}

func getScannerIdentityView(ctx context.Context, q *sqlc.Queries, libraryID, identityID int64) (ScannerV2IdentityView, error) {
	row, err := q.GetScannerIdentityForView(ctx, sqlc.GetScannerIdentityForViewParams{
		LibraryID:  libraryID,
		IdentityID: identityID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ScannerV2IdentityView{}, ErrScannerReviewTargetNotFound
		}
		return ScannerV2IdentityView{}, err
	}
	return scannerIdentityViewFromGet(row), nil
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

func addScannerBucketCount(counts *ScannerV2BucketCounts, bucket string) {
	counts.Total++
	switch bucket {
	case "matched":
		counts.Matched++
	case "needs_review":
		counts.NeedsReview++
	case "rejected":
		counts.Rejected++
	case "ignored":
		counts.Ignored++
	default:
		counts.Unmatched++
	}
}

func scannerReviewReason(reason, fallback string) string {
	if reason == "" {
		return fallback
	}
	return reason
}

func scannerCandidateView(row sqlc.ListScannerCandidatesByLibraryRow) ScannerV2CandidateView {
	return ScannerV2CandidateView{
		ID:              row.ID,
		IdentityID:      row.IdentityID,
		ScanRunID:       int8Ptr(row.ScanRunID),
		ProviderName:    row.ProviderName,
		ProviderID:      row.ProviderID,
		ProviderKind:    row.ProviderKind,
		Title:           row.Title,
		Year:            row.Year,
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
