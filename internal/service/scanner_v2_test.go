package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestScannerV2ReviewViewBucketsAndActions(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	app := &App{db: pool}
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-v2-review-test",
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
		ScannerVersion: "v2-test",
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

	view, err := app.GetLibraryScannerV2View(ctx, lib.ID, true)
	require.NoError(t, err)
	require.Equal(t, ScannerV2BucketCounts{
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

	approved, err := app.ApproveScannerV2Candidate(ctx, lib.ID, review.ID, candidate.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", approved.ReviewStatus)
	require.Equal(t, "unmatched", approved.Bucket)
	require.Equal(t, candidate.ProviderID, approved.SelectedProviderID)
	require.EqualValues(t, 0, approved.OpenFindingCount)

	rejectedView, err := app.RejectScannerV2Identity(ctx, lib.ID, unmatched.ID, "not_this")
	require.NoError(t, err)
	require.Equal(t, "rejected", rejectedView.ReviewStatus)
	require.Equal(t, "rejected", rejectedView.Bucket)

	ignoredView, err := app.IgnoreScannerV2Identity(ctx, lib.ID, rejected.ID, "")
	require.NoError(t, err)
	require.Equal(t, "ignored", ignoredView.ReviewStatus)
	require.Equal(t, "ignored", ignoredView.Bucket)

	resetView, err := app.ResetScannerV2IdentityReview(ctx, lib.ID, rejected.ID)
	require.NoError(t, err)
	require.Equal(t, "needs_review", resetView.ReviewStatus)
	require.Equal(t, "needs_review", resetView.Bucket)
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
		Source:             "scanner_v2_test",
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

func scannerBucketsByID(identities []ScannerV2IdentityView) map[int64]string {
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
