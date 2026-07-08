package scanner

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPersistScanResultPersistsMusicScannerReviewState(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-music-persistence-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/tmp/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	matchedItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID:    lib.ID,
		MediaType:    lib.MediaType,
		Title:        "Ado",
		SortTitle:    "Ado",
		ExternalIds:  []byte(`{"mbid":"ado-artist"}`),
		ProviderKind: "mbid",
	})
	require.NoError(t, err)

	result := Result{
		MusicTracks: []MusicTrackPlan{
			{
				Key:        "track:broken",
				Artist:     "Broken Artist",
				Album:      "Broken Album",
				TrackTitle: "Untitled",
				RelPath:    "Broken Artist/Broken Album/track.mp3",
				Issues:     []string{"missing_track_number"},
			},
		},
		MusicAlbums: []MusicAlbumPlan{
			{
				Key:    musicAlbumKey("Broken Artist", "Broken Album", "2026"),
				Artist: "Broken Artist",
				Album:  "Broken Album",
				Year:   "2026",
				Issues: []string{"duplicate_album_identity"},
			},
		},
		MusicArtists: []MusicArtistPlan{
			{Key: "artist:ado", Artist: "Ado", Confidence: 0.99},
			{Key: "artist:broken artist", Artist: "Broken Artist", Confidence: 0.45},
			{Key: "artist:mapping artist", Artist: "Mapping Artist", Confidence: 0.9},
			{Key: "artist:local only", Artist: "Local Only", Confidence: 0.88},
		},
		MusicSearch: []MusicSearchMatch{
			{
				Key:        "artist:ado",
				Query:      MusicSearchQuery{Artist: "Ado"},
				Accepted:   true,
				ProviderID: "heya:artist:mbid:ado-artist",
				Provider:   "heya",
				Artist:     "Ado",
				Confidence: 0.99,
				Candidates: musicCandidates("ado", "Ado", 25),
				ExternalIDs: map[string]string{
					"mbid": "ado-artist",
				},
			},
			{
				Key:        "artist:broken artist",
				Query:      MusicSearchQuery{Artist: "Broken Artist"},
				Accepted:   false,
				Reason:     "ambiguous_or_low_confidence",
				Confidence: 0.44,
				Candidates: musicCandidates("broken", "Broken Artist", 2),
			},
			{
				Key:        "artist:mapping artist",
				Query:      MusicSearchQuery{Artist: "Mapping Artist"},
				Accepted:   true,
				ProviderID: "heya:artist:mbid:mapping-artist",
				Provider:   "heya",
				Artist:     "Mapping Artist",
				Confidence: 0.9,
				Candidates: musicCandidates("mapping", "Mapping Artist", 1),
			},
		},
		MusicMetadata: []MusicFetchPreview{
			{
				Key:          "artist:mapping artist",
				ProviderID:   "heya:artist:mbid:mapping-artist",
				Artist:       "Mapping Artist",
				LocalAlbums:  2,
				MappedAlbums: 1,
				LocalTracks:  4,
				MappedTracks: 2,
				Issues:       []string{"Some Album: remote_album_not_found"},
			},
		},
		MusicApply: []MusicApplyResult{
			{
				Key:         "artist:ado",
				Action:      "update",
				Artist:      "Ado",
				ProviderID:  "heya:artist:mbid:ado-artist",
				MediaItemID: matchedItem.ID,
			},
		},
	}
	events := []Event{{Event: "nfo.parse_failed", Severity: SeverityWarn, RelPath: "Broken Artist/Broken Album/album.nfo"}}

	scanRunID, err := PersistScanResult(ctx, lib, result, events, Options{
		Apply:              true,
		FetchPreview:       true,
		MaterializePreview: true,
		RemoteSearch:       true,
	}, pool, map[string]any{"music_artists": len(result.MusicArtists)})
	require.NoError(t, err)
	require.NotZero(t, scanRunID)

	artifact, err := q.GetScanRunArtifact(ctx, sqlc.GetScanRunArtifactParams{
		ScanRunID: scanRunID,
		Kind:      scanArtifactKindSearch,
		ScopeKey:  scannerScopeKey(nil),
	})
	require.NoError(t, err)
	require.NotEmpty(t, artifact.Data)

	identities, err := q.ListScannerIdentitiesByLibrary(ctx, lib.ID)
	require.NoError(t, err)
	require.Len(t, identities, 4)
	byKey := scannerIdentitiesByKey(identities)

	require.Equal(t, "accepted", byKey["artist:ado"].ReviewStatus)
	require.True(t, byKey["artist:ado"].MediaItemID.Valid)
	require.Equal(t, matchedItem.ID, byKey["artist:ado"].MediaItemID.Int64)
	require.Equal(t, "needs_review", byKey["artist:broken artist"].ReviewStatus)
	require.Equal(t, "needs_review", byKey["artist:mapping artist"].ReviewStatus)
	require.Equal(t, "accepted", byKey["artist:local only"].ReviewStatus)

	candidates, err := q.ListScannerCandidatesByLibrary(ctx, lib.ID)
	require.NoError(t, err)
	require.Len(t, candidates, 23)
	require.Equal(t, 20, scannerCandidateCount(candidates, byKey["artist:ado"].ID))
	require.Equal(t, 2, scannerCandidateCount(candidates, byKey["artist:broken artist"].ID))
	require.Equal(t, 1, scannerCandidateCount(candidates, byKey["artist:mapping artist"].ID))

	findings, err := q.ListOpenScannerFindingsByLibrary(ctx, lib.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]int{
		"music_album_issue":      1,
		"music_metadata_mapping": 1,
		"music_track_issue":      1,
		"nfo_parse_failed":       1,
		"search_rejected":        1,
		"search_suspicious":      1,
	}, scannerFindingCounts(findings))

	approved, err := q.ApproveScannerCandidate(ctx, sqlc.ApproveScannerCandidateParams{
		LibraryID:   lib.ID,
		IdentityID:  byKey["artist:broken artist"].ID,
		CandidateID: firstCandidateID(candidates, byKey["artist:broken artist"].ID),
	})
	require.NoError(t, err)
	require.Equal(t, "accepted", approved.ReviewStatus)
	require.False(t, approved.MediaItemID.Valid, "manual approval should wait for a follow-up apply to attach media")
}

func musicCandidates(prefix string, artist string, n int) []MusicSearchCandidate {
	out := make([]MusicSearchCandidate, 0, n)
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("%s-%02d", prefix, i)
		out = append(out, MusicSearchCandidate{
			ProviderID:  "heya:artist:mbid:" + id,
			Provider:    "heya",
			Artist:      artist,
			Confidence:  1 - float64(i-1)*0.01,
			ExternalIDs: map[string]string{"mbid": id},
		})
	}
	return out
}

func scannerIdentitiesByKey(rows []sqlc.ListScannerIdentitiesByLibraryRow) map[string]sqlc.ListScannerIdentitiesByLibraryRow {
	out := make(map[string]sqlc.ListScannerIdentitiesByLibraryRow, len(rows))
	for _, row := range rows {
		out[row.IdentityKey] = row
	}
	return out
}

func scannerCandidateCount(rows []sqlc.ListScannerCandidatesByLibraryRow, identityID int64) int {
	n := 0
	for _, row := range rows {
		if row.IdentityID == identityID {
			n++
		}
	}
	return n
}

func scannerFindingCounts(rows []sqlc.ListOpenScannerFindingsByLibraryRow) map[string]int {
	out := map[string]int{}
	for _, row := range rows {
		out[row.Code]++
	}
	return out
}

func firstCandidateID(rows []sqlc.ListScannerCandidatesByLibraryRow, identityID int64) int64 {
	for _, row := range rows {
		if row.IdentityID == identityID {
			return row.ID
		}
	}
	return 0
}
