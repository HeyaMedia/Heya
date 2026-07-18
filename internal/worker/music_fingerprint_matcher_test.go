package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/acoustid"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

type staticRecordingResolver struct {
	value metadata.RecordingMetadata
}

func (r staticRecordingResolver) ResolveRecordingMBID(context.Context, string) (metadata.RecordingMetadata, error) {
	return r.value, nil
}

func TestMusicFingerprintMatcherComputesInlineAndCachesLookup(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "inline-fingerprint-matcher-test", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/music"}, ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: lib.ID, Path: "/music/$Not/Ethereal/01.flac", Size: 4321,
		Mtime:       pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true},
		ParseResult: []byte("{}"), Status: sqlc.FileStatusUnmatched,
	})
	require.NoError(t, err)
	mediaInfo, err := json.Marshal(MediaInfo{Duration: 180.6, Streams: []StreamInfo{}})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{ID: file.ID, MediaInfo: mediaInfo}))

	var lookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lookups.Add(1)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "181", r.Form.Get("duration"))
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"id":"acoustid","score":0.99,"recordings":[{"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}]}]}`))
	}))
	defer server.Close()
	client, err := acoustid.New(acoustid.Options{BaseURL: server.URL, APIKey: "app-key", RequestsPerSecond: 3, HTTPClient: server.Client()})
	require.NoError(t, err)

	original := computeChromaprint
	computeCalls := 0
	computeChromaprint = func(context.Context, string) (string, error) {
		computeCalls++
		return "AQIDBA", nil
	}
	t.Cleanup(func() { computeChromaprint = original })

	matcher := newMusicFingerprintMatcher(pool, lib, client, staticRecordingResolver{value: metadata.RecordingMetadata{
		CanonicalID: "recording", Title: "Ethereal", Duration: 180,
		ArtistCredits: []metadata.ArtistCreditEntry{{
			Name: "$Not", Slug: "20000000-0000-4000-8000-000000000001", MBID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		}},
	}})
	require.NotNil(t, matcher)
	track := scanner.MusicTrackPlan{TrackTitle: "Ethereal", RelPath: "$Not/Ethereal/01.flac"}
	first, err := matcher.MatchTrack(ctx, track)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "20000000-0000-4000-8000-000000000001", first[0].Artists[0].CanonicalID)
	second, err := matcher.MatchTrack(ctx, track)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, computeCalls)
	require.EqualValues(t, 1, lookups.Load())
}
