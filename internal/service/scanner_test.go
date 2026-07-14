package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestScannerReviewViewBucketsAndActions(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	app := &App{db: pool}
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-review-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/tmp"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	run, err := q.CreateScanRun(ctx, sqlc.CreateScanRunParams{
		LibraryID:      lib.ID,
		MediaType:      lib.MediaType,
		ScannerVersion: "scanner-test",
		Mode:           "apply",
		Status:         "complete",
		Summary:        []byte(`{"ok":true}`),
	})
	require.NoError(t, err)

	mediaItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    lib.MediaType,
		Title:        "Matched Movie",
		SortTitle:    "Matched Movie",
		Year:         "2001",
		ExternalIds:  []byte(`{"tmdb":"1001"}`),
		ProviderKind: "tmdb",
	})
	require.NoError(t, err)

	matched := createScannerIdentity(t, ctx, q, lib, run.ID, "title_year:matched|2001", "Matched Movie", "2001", "accepted", mediaItem.ID)
	review := createScannerIdentity(t, ctx, q, lib, run.ID, "title_year:review|2002", "Review Movie", "2002", "needs_review", 0)
	rejected := createScannerIdentity(t, ctx, q, lib, run.ID, "title_year:rejected|2003", "Rejected Movie", "2003", "rejected", 0)
	unmatched := createScannerIdentity(t, ctx, q, lib, run.ID, "title_year:unmatched|2004", "Unmatched Movie", "2004", "accepted", 0)
	pipelineFailure, err := q.UpsertScannerEntity(ctx, sqlc.UpsertScannerEntityParams{
		LibraryID:       lib.ID,
		MediaType:       lib.MediaType,
		ScopeKey:        "scope:broken",
		ScopePaths:      []string{"/tmp/Broken Movie"},
		IdentityKey:     "title_year:broken|2005",
		Title:           "Broken Movie",
		Year:            "2005",
		ProviderID:      "heya:broken",
		Status:          "apply_error",
		SearchScanRunID: pgInt8ForTest(run.ID),
		ErrorMessage:    "insert album: duplicate key",
		Data:            []byte(`{}`),
	})
	require.NoError(t, err)

	candidate, err := q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID:      review.ID,
		ScanRunID:       pgInt8ForTest(run.ID),
		ProviderName:    "heya",
		ProviderID:      "heya:movie:tmdb:2002",
		ProviderKind:    "tmdb",
		Title:           "Review Movie",
		Year:            "2002",
		Score:           pgNumericForTest(t, "0.920"),
		Rank:            1,
		Status:          "candidate",
		RejectionReason: "",
		ExternalIds:     []byte(`{"tmdb":"2002"}`),
		RawData:         []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID:      review.ID,
		ScanRunID:       pgInt8ForTest(run.ID),
		ProviderName:    "heya",
		ProviderID:      "heya:movie:tmdb:2999",
		ProviderKind:    "tmdb",
		Title:           "Wrong Movie",
		Year:            "2002",
		Score:           pgNumericForTest(t, "0.500"),
		Rank:            2,
		Status:          "candidate",
		RejectionReason: "",
		ExternalIds:     []byte(`{"tmdb":"2999"}`),
		RawData:         []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = q.CreateScanFinding(ctx, sqlc.CreateScanFindingParams{
		ScanRunID:  pgInt8ForTest(run.ID),
		LibraryID:  lib.ID,
		MediaType:  lib.MediaType,
		IdentityID: pgInt8ForTest(review.ID),
		Severity:   "warn",
		Code:       "search_suspicious",
		Message:    "selected search result needs review",
		Data:       []byte(`{}`),
	})
	require.NoError(t, err)

	view, err := app.GetLibraryScannerView(ctx, lib.ID, true)
	require.NoError(t, err)
	require.Equal(t, ScannerBucketCounts{
		Total:       4,
		Matched:     1,
		NeedsReview: 1,
		Rejected:    1,
		Unmatched:   1,
	}, view.BucketCounts)
	require.Equal(t, map[int64]string{
		matched.ID:   "matched",
		review.ID:    "needs_review",
		rejected.ID:  "rejected",
		unmatched.ID: "unmatched",
	}, scannerBucketsByID(view.Identities))
	require.Len(t, view.Candidates, 2)
	require.Len(t, view.OpenFindings, 1)
	require.Equal(t, []ScannerPipelineFailureView{{
		ID:           pipelineFailure.ID,
		IdentityKey:  pipelineFailure.IdentityKey,
		Title:        pipelineFailure.Title,
		Status:       pipelineFailure.Status,
		Stage:        "metadata apply",
		ErrorMessage: pipelineFailure.ErrorMessage,
		UpdatedAt:    timePtr(pipelineFailure.UpdatedAt),
	}}, view.PipelineFailures)
	require.NotNil(t, view.LatestRun)
	require.Equal(t, 1, view.LatestRun.PipelineFailureCount)
	require.Equal(t, pipelineFailure.ErrorMessage, view.LatestRun.PipelineErrorMessage)

	runs, err := app.ListLibraryScannerRuns(ctx, lib.ID, 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, runs)
	require.Equal(t, 1, runs[0].PipelineFailureCount)
	require.Equal(t, pipelineFailure.ErrorMessage, runs[0].PipelineErrorMessage)

	approved, err := app.ApproveScannerCandidate(ctx, lib.ID, review.ID, candidate.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", approved.ReviewStatus)
	require.Equal(t, "unmatched", approved.Bucket)
	require.Equal(t, candidate.ProviderID, approved.SelectedProviderID)
	require.EqualValues(t, 0, approved.OpenFindingCount)

	rejectedView, err := app.RejectScannerIdentity(ctx, lib.ID, unmatched.ID, "not_this")
	require.NoError(t, err)
	require.Equal(t, "rejected", rejectedView.ReviewStatus)
	require.Equal(t, "rejected", rejectedView.Bucket)

	ignoredView, err := app.IgnoreScannerIdentity(ctx, lib.ID, rejected.ID, "")
	require.NoError(t, err)
	require.Equal(t, "ignored", ignoredView.ReviewStatus)
	require.Equal(t, "ignored", ignoredView.Bucket)

	resetView, err := app.ResetScannerIdentityReview(ctx, lib.ID, rejected.ID)
	require.NoError(t, err)
	require.Equal(t, "needs_review", resetView.ReviewStatus)
	require.Equal(t, "needs_review", resetView.Bucket)

	// Bulk approval must count every candidate, not only candidates above the
	// threshold: the two-candidate review identity is deliberately ineligible.
	bulk, err := app.BulkApproveSingleScannerCandidates(ctx, lib.ID, 0.9)
	require.NoError(t, err)
	require.Zero(t, bulk.Approved)

	single := createScannerIdentity(t, ctx, q, lib, run.ID, "title_year:single|2005", "Single Movie", "2005", "needs_review", 0)
	_, err = q.UpsertMetadataMatchCandidate(ctx, sqlc.UpsertMetadataMatchCandidateParams{
		IdentityID: single.ID, ScanRunID: pgInt8ForTest(run.ID), ProviderName: "heya",
		ProviderID: "heya:movie:tmdb:2005", ProviderKind: "tmdb", Title: "Single Movie", Year: "2005",
		Score: pgNumericForTest(t, "0.950"), Rank: 1, Status: "selected", RejectionReason: "",
		ExternalIds: []byte(`{"tmdb":"2005"}`), RawData: []byte(`{}`),
	})
	require.NoError(t, err)

	bulk, err = app.BulkApproveSingleScannerCandidates(ctx, lib.ID, 0.95)
	require.NoError(t, err)
	require.Equal(t, 1, bulk.Approved)
	bulkApproved, err := getScannerIdentityView(ctx, q, lib.ID, single.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", bulkApproved.ReviewStatus)
	require.Equal(t, "heya:movie:tmdb:2005", bulkApproved.SelectedProviderID)
}

func TestScannerReviewScopePaths(t *testing.T) {
	t.Run("movie folder", func(t *testing.T) {
		raw := []byte(`{"files":["Dune (2021)/Dune.2021.mkv","Dune (2021)/poster.jpg"]}`)
		require.Equal(t, []string{"/library/movies/Dune (2021)"}, scannerReviewScopePaths([]string{"/library/movies"}, raw))
	})

	t.Run("show root from seasons", func(t *testing.T) {
		raw := []byte(`{"files":["Slow Horses (2022)/Season 01/S01E01.mkv","Slow Horses (2022)/Season 02/S02E01.mkv"]}`)
		require.Equal(t, []string{"/library/tv/Slow Horses (2022)"}, scannerReviewScopePaths([]string{"/library/tv"}, raw))
	})

	t.Run("top level loose file", func(t *testing.T) {
		raw := []byte(`{"files":["Poker.Face.S01E01.mkv"]}`)
		require.Equal(t, []string{"/library/tv"}, scannerReviewScopePaths([]string{"/library/tv"}, raw))
	})

	t.Run("multiple roots", func(t *testing.T) {
		raw := []byte(`{"plans":[{"files":["ano/2022 - Chu,Tayousei./01.flac"]}]}`)
		require.Equal(t, []string{"/a/music/ano/2022 - Chu,Tayousei.", "/b/music/ano/2022 - Chu,Tayousei."}, scannerReviewScopePaths([]string{"/b/music", "/a/music"}, raw))
	})

	t.Run("no file evidence", func(t *testing.T) {
		require.Nil(t, scannerReviewScopePaths([]string{"/library/movies"}, []byte(`{"title":"Dune"}`)))
	})
}

func createScannerIdentity(t *testing.T, ctx context.Context, q *sqlc.Queries, lib sqlc.Library, runID int64, key, title, year, reviewStatus string, mediaItemID int64) sqlc.LocalMediaIdentity {
	t.Helper()
	identity, err := q.UpsertLocalMediaIdentity(ctx, sqlc.UpsertLocalMediaIdentityParams{
		LibraryID:          lib.ID,
		MediaType:          lib.MediaType,
		IdentityKey:        key,
		Title:              title,
		Year:               year,
		Confidence:         0.9,
		Source:             "scanner_test",
		ReviewStatus:       reviewStatus,
		MetadataProviderID: "",
		MediaItemID:        pgInt8ForTest(mediaItemID),
		FirstSeenScanRunID: pgInt8ForTest(runID),
		LastSeenScanRunID:  pgInt8ForTest(runID),
		RawIdentity:        []byte(`{}`),
	})
	require.NoError(t, err)
	return identity
}

func scannerBucketsByID(identities []ScannerIdentityView) map[int64]string {
	out := make(map[int64]string, len(identities))
	for _, identity := range identities {
		out[identity.ID] = identity.Bucket
	}
	return out
}

func pgInt8ForTest(value int64) pgtype.Int8 {
	if value == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: value, Valid: true}
}

func pgNumericForTest(t *testing.T, value string) pgtype.Numeric {
	t.Helper()
	var numeric pgtype.Numeric
	require.NoError(t, numeric.Scan(value))
	return numeric
}
