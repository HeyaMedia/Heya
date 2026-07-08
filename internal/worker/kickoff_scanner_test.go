package worker

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/stretchr/testify/require"
)

func TestKickoffLibraryScanSupportsScannerDomains(t *testing.T) {
	for _, mt := range []sqlc.MediaType{sqlc.MediaTypeMovie, sqlc.MediaTypeTv, sqlc.MediaTypeAnime, sqlc.MediaTypeMusic, sqlc.MediaTypeBook} {
		require.True(t, supportsScanner(mt), "%s should use scanner", mt)
	}

	for _, mt := range []sqlc.MediaType{sqlc.MediaTypeComic, sqlc.MediaTypePodcast, sqlc.MediaTypeRadio} {
		require.False(t, supportsScanner(mt), "%s should not fall back to the legacy scanner", mt)
	}
}

func TestScannerInventoryPostApplyPaths(t *testing.T) {
	inv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
		Root: "/media",
		Files: []scanner.InventoryFile{
			{Path: "/media/Movie (2021)/Movie (2021).mkv", Class: scanner.ClassPrimaryMedia},
			{Path: "/media/Movie (2021)/trailers/trailer.mp4", Class: scanner.ClassExtraMedia},
			{Path: "/media/Movie (2021)/subtitles/en.srt", Class: scanner.ClassSubtitle},
			{Path: "/media/Movie (2021)/poster.jpg", Class: scanner.ClassArtwork},
			{Path: "/media/Music/Album/01 Track.flac", Class: scanner.ClassPrimaryMedia},
			{Path: "/media/Music/Album/01 Track.flac", Class: scanner.ClassPrimaryMedia},
		},
	}}}

	require.Equal(t, []string{
		"/media/Movie (2021)/Movie (2021).mkv",
		"/media/Movie (2021)/trailers/trailer.mp4",
		"/media/Music/Album/01 Track.flac",
	}, scannerInventoryPostApplyPaths(inv))
}

func TestCompactScannerScopesDropsChildren(t *testing.T) {
	require.Equal(t, []string{
		"/library/Movie (2021)",
		"/library/Other (2022)",
	}, compactScannerScopes([]string{
		"/library/Movie (2021)",
		"/library/Movie (2021)/trailers",
		"/library/Movie (2021)/featurettes",
		"/library/Other (2022)",
	}))
}

func TestScannerScopeForPathUsesOwningMediaDirectory(t *testing.T) {
	require.Equal(t,
		"/library/Show (2024)",
		ScannerScopeForPath(sqlc.MediaTypeTv, "/library/Show (2024)/Season 01/Show.S01E01.mkv"),
	)
	require.Equal(t,
		"/library/Show (2024)",
		ScannerScopeForPath(sqlc.MediaTypeAnime, "/library/Show (2024)/Season 01/featurettes/Behind The Scenes.mkv"),
	)
	require.Equal(t,
		"/library/Movie (2024)",
		ScannerScopeForPath(sqlc.MediaTypeMovie, "/library/Movie (2024)/trailers/trailer.mkv"),
	)
	require.Equal(t,
		"/library/Music/Samples",
		ScannerScopeForPath(sqlc.MediaTypeMusic, "/library/Music/Samples/01 Track.flac"),
	)
}

func TestLibraryFileNeedsProbe(t *testing.T) {
	require.True(t, libraryFileNeedsProbe(sqlc.LibraryFile{}))
	require.True(t, libraryFileNeedsProbe(sqlc.LibraryFile{MediaInfo: []byte("{}")}))
	require.True(t, libraryFileNeedsProbe(sqlc.LibraryFile{MediaInfo: []byte(" null ")}))
	require.False(t, libraryFileNeedsProbe(sqlc.LibraryFile{MediaInfo: []byte(`{"format":{}}`)}))
}

func TestLibraryFileHasVideo(t *testing.T) {
	require.False(t, libraryFileHasVideo(sqlc.LibraryFile{}))
	require.False(t, libraryFileHasVideo(sqlc.LibraryFile{MediaInfo: []byte(`{"streams":[{"codec_type":"audio"}]}`)}))
	require.True(t, libraryFileHasVideo(sqlc.LibraryFile{MediaInfo: []byte(`{"streams":[{"codec_type":"video"}]}`)}))
}

func TestScannerMediaTypeSideEffects(t *testing.T) {
	require.True(t, scannerMediaTypeFetchesRatings(sqlc.MediaTypeMovie))
	require.True(t, scannerMediaTypeFetchesRatings(sqlc.MediaTypeBook))
	require.False(t, scannerMediaTypeFetchesRatings(sqlc.MediaTypeMusic))

	require.True(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeMovie))
	require.True(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeTv))
	require.True(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeAnime))
	require.False(t, scannerMediaTypeWritesVideoNFO(sqlc.MediaTypeBook))

	require.True(t, scannerMediaTypeScansSegments(sqlc.MediaTypeMovie))
	require.True(t, scannerMediaTypeScansSegments(sqlc.MediaTypeTv))
	require.True(t, scannerMediaTypeScansSegments(sqlc.MediaTypeAnime))
	require.False(t, scannerMediaTypeScansSegments(sqlc.MediaTypeMusic))
}

func TestLibraryFileHasPrimaryLink(t *testing.T) {
	require.False(t, libraryFileHasPrimaryLink(nil))
	require.False(t, libraryFileHasPrimaryLink([]sqlc.LibraryFileLink{{RelationType: "extra"}}))
	require.True(t, libraryFileHasPrimaryLink([]sqlc.LibraryFileLink{{RelationType: "episode"}}))
	require.True(t, libraryFileHasPrimaryLink([]sqlc.LibraryFileLink{{RelationType: "part"}}))
}

func TestShouldSaveImageSidecar(t *testing.T) {
	require.True(t, ShouldSaveImageSidecar("poster", 0, ""))
	require.True(t, ShouldSaveImageSidecar("clearart", 0, ""))
	require.True(t, ShouldSaveImageSidecar("banner", 0, ""))
	require.True(t, ShouldSaveImageSidecar("logo", 0, ""))
	require.True(t, ShouldSaveImageSidecar("thumb", 0, ""))
	require.True(t, ShouldSaveImageSidecar("backdrop", 0, ""))
	require.True(t, ShouldSaveImageSidecar("backdrop", 4, "en"))

	require.False(t, ShouldSaveImageSidecar("poster", 1001, "season-1"))
	require.False(t, ShouldSaveImageSidecar("still", 2001, "s01e01"))
	require.False(t, ShouldSaveImageSidecar("logo", 1, ""))
	require.False(t, ShouldSaveImageSidecar("backdrop", 1000, "season-1"))
}

func TestTrackFileNeedsLoudness(t *testing.T) {
	require.True(t, trackFileNeedsLoudness(sqlc.TrackFile{}))
	require.True(t, trackFileNeedsLoudness(sqlc.TrackFile{
		IntegratedLufs: pgtype.Numeric{Valid: true},
	}))
	require.True(t, trackFileNeedsLoudness(sqlc.TrackFile{
		BoundariesAnalyzedAt: pgtype.Timestamptz{Valid: true},
	}))
	require.False(t, trackFileNeedsLoudness(sqlc.TrackFile{
		IntegratedLufs:       pgtype.Numeric{Valid: true},
		BoundariesAnalyzedAt: pgtype.Timestamptz{Valid: true},
	}))
}
