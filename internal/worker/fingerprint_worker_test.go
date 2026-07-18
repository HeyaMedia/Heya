package worker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// normalizeChromaprint must map every base64 dialect the two extractors emit
// onto the AcoustID convention (URL-safe, no padding) so DB values compare
// equal regardless of which tool produced them.
func TestNormalizeChromaprint(t *testing.T) {
	// Bytes that force both dialects to differ: 0xfb 0xef encodes to "++8="
	// (std) vs "--8" (url-safe), so mixing them up cannot pass by accident.
	raw := []byte{0xfb, 0xef, 0xbe, 0x01, 0x02, 0x03, 0xff}
	want := base64.RawURLEncoding.EncodeToString(raw)

	cases := map[string]string{
		"url-safe no padding (fpcalc)": base64.RawURLEncoding.EncodeToString(raw),
		"standard padded (ffmpeg)":     base64.StdEncoding.EncodeToString(raw),
		"standard unpadded":            base64.RawStdEncoding.EncodeToString(raw),
	}
	for name, in := range cases {
		got, err := normalizeChromaprint(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}

	if _, err := normalizeChromaprint(""); err == nil {
		t.Error("empty fingerprint should error")
	}
	if _, err := normalizeChromaprint("not!!valid@@base64"); err == nil {
		t.Error("invalid base64 should error")
	}
}

func TestEnsureLibraryFileFingerprintComputesOnDemandAndCaches(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "fingerprint-on-demand-test", MediaType: sqlc.MediaTypeMusic,
		Paths: []string{"/music"}, ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy: userID, Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: lib.ID, Path: "/music/Uncertain/01 - Example.flac", Size: 1234,
		Mtime:       pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true},
		ParseResult: []byte("{}"), Status: sqlc.FileStatusUnmatched,
	})
	require.NoError(t, err)
	mediaInfo, err := json.Marshal(mediaprobe.MediaInfo{Duration: 181.4, Streams: []mediaprobe.StreamInfo{}})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileMediaInfo(ctx, sqlc.UpdateLibraryFileMediaInfoParams{ID: file.ID, MediaInfo: mediaInfo}))
	file, err = q.GetLibraryFileByID(ctx, file.ID)
	require.NoError(t, err)

	original := computeChromaprint
	calls := 0
	computeChromaprint = func(context.Context, string) (string, error) {
		calls++
		return "AQIDBA", nil
	}
	t.Cleanup(func() { computeChromaprint = original })

	first, err := ensureLibraryFileFingerprint(ctx, q, file, 0)
	require.NoError(t, err)
	require.Equal(t, "AQIDBA", first.Fingerprint)
	require.EqualValues(t, 181, first.SourceDurationSecs)
	require.EqualValues(t, chromaprintWindowSecs, first.FingerprintDurationSecs)
	second, err := ensureLibraryFileFingerprint(ctx, q, file, 0)
	require.NoError(t, err)
	require.Equal(t, first.Fingerprint, second.Fingerprint)
	require.Equal(t, 1, calls, "the second uncertainty pass must reuse the durable file fingerprint")
}
