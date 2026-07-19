package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/acoustid"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

type staticRecordingResolver struct {
	value metadata.RecordingMetadata
}

type recordingResolverFunc func(context.Context, string) (metadata.RecordingMetadata, error)

func (f recordingResolverFunc) ResolveRecordingMBID(ctx context.Context, mbid string) (metadata.RecordingMetadata, error) {
	return f(ctx, mbid)
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
	mediaInfo, err := json.Marshal(mediaprobe.MediaInfo{Duration: 180.6, Streams: []mediaprobe.StreamInfo{}})
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
		CanonicalID: "30000000-0000-4000-8000-000000000001", Title: "Ethereal", Duration: 180,
		ArtistCredits: []metadata.ArtistCreditEntry{{
			Name: "$Not", Slug: "20000000-0000-4000-8000-000000000001", MBID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		}},
	}})
	require.NotNil(t, matcher)
	track := scanner.MusicTrackPlan{TrackTitle: "Ethereal", RelPath: "$Not/Ethereal/01.flac"}
	first, err := matcher.MatchTrack(ctx, track)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "30000000-0000-4000-8000-000000000001", first[0].CanonicalRecordingID)
	require.Equal(t, "20000000-0000-4000-8000-000000000001", first[0].Artists[0].CanonicalID)
	second, err := matcher.MatchTrack(ctx, track)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, computeCalls)
	require.EqualValues(t, 1, lookups.Load())
}

func TestMusicFingerprintMatcherRefusesDuplicateRelativePathAcrossRoots(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "ambiguous-root-fingerprint-test", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/music-a", "/music-b"}, ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	relPath := "Same Artist/Same Album/01.flac"
	for _, root := range lib.Paths {
		_, err = q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
			LibraryID: lib.ID, Path: root + "/" + relPath, Size: 4321,
			Mtime:       pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true},
			ParseResult: []byte("{}"), Status: sqlc.FileStatusUnmatched,
		})
		require.NoError(t, err)
	}

	var lookups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lookups.Add(1)
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
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
		CanonicalID: "recording", Title: "Song", Duration: 180,
		ArtistCredits: []metadata.ArtistCreditEntry{{Name: "Artist", Slug: "artist"}},
	}})
	require.NotNil(t, matcher)
	evidence, err := matcher.MatchTrack(ctx, scanner.MusicTrackPlan{TrackTitle: "Song", RelPath: relPath})
	require.NoError(t, err)
	require.Empty(t, evidence, "ambiguous root-relative identity must not produce acoustic autoaccept evidence")
	require.Zero(t, computeCalls, "ambiguity must be detected before fingerprinting either file")
	require.Zero(t, lookups.Load(), "ambiguity must not poison an AcoustID cache for an arbitrary root")
}

func TestResolveAcoustIDEvidenceKeepsValidHigherRankedRecording(t *testing.T) {
	matches := []acoustid.Match{
		{RecordingMBID: "high", Score: .99},
		{RecordingMBID: "lower", Score: .95},
	}
	resolver := recordingResolverFunc(func(_ context.Context, mbid string) (metadata.RecordingMetadata, error) {
		if mbid == "lower" {
			return metadata.RecordingMetadata{}, errors.New("lower recording resolution unavailable")
		}
		return metadata.RecordingMetadata{
			CanonicalID: "30000000-0000-4000-8000-000000000001", Title: "Song", Duration: 180,
			ArtistCredits: []metadata.ArtistCreditEntry{{Name: "Artist", Slug: "canonical", MBID: "artist-mbid"}},
		}, nil
	})
	evidence, err := resolveAcoustIDRecordingEvidence(context.Background(), matches, sqlc.LibraryFileFingerprint{SourceDurationSecs: 181}, resolver)
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, "high", evidence[0].RecordingMBID)
	require.Equal(t, "30000000-0000-4000-8000-000000000001", evidence[0].CanonicalRecordingID)
	require.Equal(t, "canonical", evidence[0].Artists[0].CanonicalID)
}

func TestAcoustIDTransientFailureBecomesDeferredWork(t *testing.T) {
	upstream := &acoustid.LookupError{Class: acoustid.ErrorTransient, Message: "service unavailable", RetryAfter: 23 * time.Second}
	err := scannerAcoustIDLookupError(upstream, acoustid.ErrorRetryAfter(upstream))
	retryAfter, deferred := metadata.DeferredWorkRetryAfter(err)
	require.True(t, deferred)
	require.Equal(t, 23*time.Second, retryAfter)

	configuration := &acoustid.LookupError{Class: acoustid.ErrorConfiguration, Message: "invalid client key"}
	_, deferred = metadata.DeferredWorkRetryAfter(scannerAcoustIDLookupError(configuration, time.Hour))
	require.False(t, deferred)
}

func TestCachedAcoustIDTransientFailureRemainsDeferred(t *testing.T) {
	body, err := json.Marshal(acoustIDFailureRecord{Class: acoustid.ErrorTransient})
	require.NoError(t, err)
	err = cachedAcoustIDLookupError("service unavailable", body, 31*time.Second)
	retryAfter, deferred := metadata.DeferredWorkRetryAfter(err)
	require.True(t, deferred)
	require.Equal(t, 31*time.Second, retryAfter)
}

func TestLegacyUntypedAcoustIDFailureIsRetried(t *testing.T) {
	require.NoError(t, cachedAcoustIDLookupError("acoustid lookup: HTTP 400", []byte("[]"), time.Hour))
}
