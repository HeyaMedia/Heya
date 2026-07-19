package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/secrettext"
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
		ErrorMessage:    "open https://reader:super-secret@storage.test/share: duplicate key",
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
	// Orphan finding (no identity) — must surface via issue counts + the paged
	// issues list, never via identity findings.
	_, err = q.CreateScanFinding(ctx, sqlc.CreateScanFindingParams{
		ScanRunID: pgInt8ForTest(run.ID),
		LibraryID: lib.ID,
		MediaType: lib.MediaType,
		Severity:  "warn",
		Code:      "unplanned_media",
		Message:   "stray file",
		Data:      []byte(`{}`),
	})
	require.NoError(t, err)

	overview, err := app.GetLibraryScannerOverview(ctx, lib.ID)
	require.NoError(t, err)
	require.Equal(t, ScannerBucketCounts{
		Total:       4,
		Matched:     1,
		NeedsReview: 1,
		Rejected:    1,
		Unmatched:   1,
	}, overview.BucketCounts)
	require.Equal(t, []ScannerIssueCount{{Code: "unplanned_media", Severity: "warn", Count: 1}}, overview.IssueCounts)
	require.EqualValues(t, 1, overview.IssueTotal)
	redactedPipelineError := secrettext.Redact(pipelineFailure.ErrorMessage)
	require.NotContains(t, redactedPipelineError, "super-secret")
	require.Equal(t, []ScannerPipelineFailureView{{
		ID:           pipelineFailure.ID,
		IdentityKey:  pipelineFailure.IdentityKey,
		Title:        pipelineFailure.Title,
		Status:       pipelineFailure.Status,
		Stage:        "metadata apply",
		ErrorMessage: redactedPipelineError,
		UpdatedAt:    timePtr(pipelineFailure.UpdatedAt),
	}}, overview.PipelineFailures)
	require.NotNil(t, overview.LatestRun)
	require.Equal(t, 1, overview.LatestRun.PipelineFailureCount)
	require.Equal(t, redactedPipelineError, overview.LatestRun.PipelineErrorMessage)

	identities, err := app.ListScannerIdentitiesPage(ctx, lib.ID, "", "", 50, 0)
	require.NoError(t, err)
	require.Equal(t, map[int64]string{
		matched.ID:   "matched",
		review.ID:    "needs_review",
		rejected.ID:  "rejected",
		unmatched.ID: "unmatched",
	}, scannerBucketsByID(identities))

	reviewPage, err := app.ListScannerIdentitiesPage(ctx, lib.ID, "needs_review", "", 50, 0)
	require.NoError(t, err)
	require.Len(t, reviewPage, 1)
	require.Equal(t, review.ID, reviewPage[0].ID)
	require.Equal(t, "search_suspicious", reviewPage[0].MainFindingCode)
	require.EqualValues(t, 2, reviewPage[0].CandidateCount)

	searchPage, err := app.ListScannerIdentitiesPage(ctx, lib.ID, "", "unmatched movie", 50, 0)
	require.NoError(t, err)
	require.Len(t, searchPage, 1)
	require.Equal(t, unmatched.ID, searchPage[0].ID)

	pagedIdentities, err := app.ListScannerIdentitiesPage(ctx, lib.ID, "", "", 3, 3)
	require.NoError(t, err)
	require.Len(t, pagedIdentities, 1)

	reviewCandidates, err := app.ListScannerIdentityCandidates(ctx, lib.ID, review.ID)
	require.NoError(t, err)
	require.Len(t, reviewCandidates, 2)
	require.Equal(t, candidate.ProviderID, reviewCandidates[0].ProviderID)

	reviewFindings, err := app.ListScannerIdentityFindings(ctx, lib.ID, review.ID, 50, 0)
	require.NoError(t, err)
	require.Len(t, reviewFindings, 1)
	require.Equal(t, "search_suspicious", reviewFindings[0].Code)

	issues, err := app.ListScannerIssuesPage(ctx, lib.ID, "", 50, 0)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, "unplanned_media", issues[0].Code)
	filteredIssues, err := app.ListScannerIssuesPage(ctx, lib.ID, "nfo_parse_failed", 50, 0)
	require.NoError(t, err)
	require.Empty(t, filteredIssues)

	runs, err := app.ListLibraryScannerRuns(ctx, lib.ID, 10, 0)
	require.NoError(t, err)
	require.NotEmpty(t, runs)
	require.Equal(t, 1, runs[0].PipelineFailureCount)
	require.Equal(t, redactedPipelineError, runs[0].PipelineErrorMessage)

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
	eligible, err := app.CountScannerBulkApproveEligible(ctx, lib.ID, 0.9)
	require.NoError(t, err)
	require.Zero(t, eligible)
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

	eligible, err = app.CountScannerBulkApproveEligible(ctx, lib.ID, 0.95)
	require.NoError(t, err)
	require.EqualValues(t, 1, eligible)
	bulk, err = app.BulkApproveSingleScannerCandidates(ctx, lib.ID, 0.95)
	require.NoError(t, err)
	require.Equal(t, 1, bulk.Approved)
	bulkApproved, err := getScannerIdentityView(ctx, q, lib.ID, single.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", bulkApproved.ReviewStatus)
	require.Equal(t, "heya:movie:tmdb:2005", bulkApproved.SelectedProviderID)
}

func TestTerminalReviewActionsRefuseAlreadyAppliedIdentity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	app := &App{db: pool}
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "scanner-bound-review-test", MediaType: sqlc.MediaTypeMovie,
		Paths:        []string{"/tmp/scanner-bound-review-test"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: lib.ID, MediaType: lib.MediaType, Title: "Already Applied", SortTitle: "already applied",
	})
	require.NoError(t, err)
	identity := createScannerIdentity(t, ctx, q, lib, 0, "title_year:already applied|2026", "Already Applied", "2026", "accepted", item.ID)

	_, err = app.RejectScannerIdentity(ctx, lib.ID, identity.ID, "wrong match")
	require.ErrorIs(t, err, ErrScannerReviewIdentityApplied)
	_, err = app.IgnoreScannerIdentity(ctx, lib.ID, identity.ID, "hide")
	require.ErrorIs(t, err, ErrScannerReviewIdentityApplied)

	current, err := q.GetScannerIdentityForView(ctx, sqlc.GetScannerIdentityForViewParams{LibraryID: lib.ID, IdentityID: identity.ID})
	require.NoError(t, err)
	require.Equal(t, "accepted", current.ReviewStatus, "terminal review must not disagree with the still-served media item")
	require.True(t, current.MediaItemID.Valid)
	require.Equal(t, item.ID, current.MediaItemID.Int64)
}

func TestBookManualIdentitySearchRetainsAuthorFormatAndISBN(t *testing.T) {
	var discovery map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v2/discoveries", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&discovery))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"99999999-9999-4999-8999-999999999999",
			"state":"completed",
			"expires_at":"2099-01-01T00:00:00Z",
			"result":{
				"kind":"book_work","query":"The Long Winter","recommendation":"no_match",
				"status":"completed","schema_version":1,"observed_at":"2026-07-19T00:00:00Z","candidates":[]
			}
		}`))
	}))
	defer server.Close()
	client, err := heyametadata.NewClient(server.URL, "")
	require.NoError(t, err)

	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	app := &App{db: pool, heya: heyametadata.NewHeyaProvider(client)}
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "book-manual-search-evidence", MediaType: sqlc.MediaTypeBook, Paths: []string{"/books"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	identity := createScannerIdentity(t, ctx, q, lib, 0, "book:audiobook|a g riddle|the long winter|", "The Long Winter", "", "needs_review", 0)
	_, err = pool.Exec(ctx, `
		UPDATE local_media_identities
		SET raw_identity = $2
		WHERE id = $1
	`, identity.ID, []byte(`{"title":"The Long Winter","author":"A. G. Riddle","format":"audiobook","external_ids":{"isbn_13":"9780000000002"}}`))
	require.NoError(t, err)

	_, err = app.SearchScannerIdentity(ctx, lib.ID, identity.ID, "", "")
	require.NoError(t, err)
	require.Equal(t, "book_work", discovery["kind"])
	hints, ok := discovery["hints"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"A. G. Riddle"}, hints["authors"])
	require.Equal(t, []any{"9780000000002"}, hints["isbns"])
	require.Equal(t, "audiobook", hints["type"])
	identifiers, ok := discovery["identifiers"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, identifiers)
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
