package worker

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
)

func TestOversizedScannerArtifactCancelsWorkerRetry(t *testing.T) {
	err := &scanner.ArtifactTooLargeError{Kind: "search_result", Size: 17, Limit: 16}
	got := scannerWorkerError(err)

	require.ErrorIs(t, got, river.JobCancel(errors.New("permanent")))
	require.ErrorIs(t, got, err)
}

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

func TestScannerScopeForInventoryFileKeepsTopLevelMediaFileScoped(t *testing.T) {
	file := scanner.InventoryFile{
		Path:    "/library/Loose.Movie.2024.1080p.WEB-DL.mkv",
		RelPath: "Loose.Movie.2024.1080p.WEB-DL.mkv",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, file.Path, scannerScopeForInventoryFile(sqlc.MediaTypeMovie, file))

	nested := scanner.InventoryFile{
		Path:    "/library/Movie (2024)/Movie.2024.mkv",
		RelPath: "Movie (2024)/Movie.2024.mkv",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, "/library/Movie (2024)", scannerScopeForInventoryFile(sqlc.MediaTypeMovie, nested))
}

func TestScannerScopeForInventoryFileUsesMusicArtistScope(t *testing.T) {
	albumTrack := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/ano/2022 - Chu,Tayousei./01 - Chu,Tayousei.flac",
		RelPath: "ano/2022 - Chu,Tayousei./01 - Chu,Tayousei.flac",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, "/library/Music/ano", scannerScopeForInventoryFile(sqlc.MediaTypeMusic, albumTrack))

	artistTrack := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/ano/01 - Loose.flac",
		RelPath: "ano/01 - Loose.flac",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, "/library/Music/ano", scannerScopeForInventoryFile(sqlc.MediaTypeMusic, artistTrack))

	looseTrack := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/loose.mp3",
		RelPath: "loose.mp3",
		Class:   scanner.ClassPrimaryMedia,
	}
	require.Equal(t, looseTrack.Path, scannerScopeForInventoryFile(sqlc.MediaTypeMusic, looseTrack))

	albumNFO := scanner.InventoryFile{
		Root:    "/library/Music",
		Path:    "/library/Music/ano/2022 - Chu,Tayousei./album.nfo",
		RelPath: "ano/2022 - Chu,Tayousei./album.nfo",
		Class:   scanner.ClassNFO,
	}
	require.Equal(t, "/library/Music/ano", scannerScopeForInventoryFile(sqlc.MediaTypeMusic, albumNFO))
}

func TestScannerScopeForLibraryPathUsesMusicArtistScope(t *testing.T) {
	lib := sqlc.Library{
		MediaType: sqlc.MediaTypeMusic,
		Paths:     []string{"/library/Music"},
	}

	require.Equal(t,
		"/library/Music/Daft Punk",
		ScannerScopeForLibraryPath(lib, "/library/Music/Daft Punk/1997 - Homework/01 - Daftendirekt.flac"),
	)
	require.Equal(t,
		"/library/Music/Daft Punk",
		ScannerScopeForLibraryPath(lib, "/library/Music/Daft Punk/1997 - Homework/album.nfo"),
	)
	require.Equal(t,
		"/library/Music/Daft Punk",
		ScannerScopeForLibraryPath(lib, "/library/Music/Daft Punk"),
	)
}

func TestScannerScopeForLibraryDirectoryKeepsNFOOwnerScope(t *testing.T) {
	tests := []struct {
		name string
		lib  sqlc.Library
		dir  string
		want string
	}{
		{
			name: "TV show",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeTv, Paths: []string{"/storage/TV/Foreign"}},
			dir:  "/storage/TV/Foreign/Some Show",
			want: "/storage/TV/Foreign/Some Show",
		},
		{
			name: "TV season promotes to show",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeTv, Paths: []string{"/storage/TV/Foreign"}},
			dir:  "/storage/TV/Foreign/Some Show/Season 01",
			want: "/storage/TV/Foreign/Some Show",
		},
		{
			name: "movie",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeMovie, Paths: []string{"/storage/Movies"}},
			dir:  "/storage/Movies/Dune (2021)",
			want: "/storage/Movies/Dune (2021)",
		},
		{
			name: "SMB TV show",
			lib:  sqlc.Library{MediaType: sqlc.MediaTypeTv, Paths: []string{"smb://nas/media/TV"}},
			dir:  "smb://nas/media/TV/Some Show",
			want: "smb://nas/media/TV/Some Show",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ScannerScopeForLibraryDirectory(tt.lib, tt.dir))
		})
	}
}

func TestProcessLibraryScanFanoutSplitsFullAndRootScopesIntoOwners(t *testing.T) {
	tests := []struct {
		name      string
		lib       sqlc.Library
		inventory scanner.Inventory
		want      []string
	}{
		{
			name: "local TV",
			lib: sqlc.Library{
				ID:        3,
				MediaType: sqlc.MediaTypeTv,
				Paths:     []string{"/storage/TV/Foreign"},
			},
			inventory: scanner.Inventory{Roots: []scanner.InventoryRoot{{
				Root: "/storage/TV/Foreign",
				Files: []scanner.InventoryFile{
					{Root: "/storage/TV/Foreign", Path: "/storage/TV/Foreign/Alpha/Season 01/Alpha.S01E01.mkv", RelPath: "Alpha/Season 01/Alpha.S01E01.mkv", Class: scanner.ClassPrimaryMedia},
					{Root: "/storage/TV/Foreign", Path: "/storage/TV/Foreign/Beta/Season 02/Beta.S02E01.mkv", RelPath: "Beta/Season 02/Beta.S02E01.mkv", Class: scanner.ClassPrimaryMedia},
				},
			}}},
			want: []string{"/storage/TV/Foreign/Alpha", "/storage/TV/Foreign/Beta"},
		},
		{
			name: "SMB movies",
			lib: sqlc.Library{
				ID:        4,
				MediaType: sqlc.MediaTypeMovie,
				Paths:     []string{"smb://nas/media/Movies"},
			},
			inventory: scanner.Inventory{Roots: []scanner.InventoryRoot{{
				Root: "smb://nas/media/Movies",
				Files: []scanner.InventoryFile{
					{Root: "smb://nas/media/Movies", Path: "smb://nas/media/Movies/Alien (1979)/Alien.mkv", RelPath: "Alien (1979)/Alien.mkv", Class: scanner.ClassPrimaryMedia},
					{Root: "smb://nas/media/Movies", Path: "smb://nas/media/Movies/Dune (2021)/Dune.mkv", RelPath: "Dune (2021)/Dune.mkv", Class: scanner.ClassPrimaryMedia},
				},
			}}},
			want: []string{"smb://nas/media/Movies/Alien (1979)", "smb://nas/media/Movies/Dune (2021)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := ProcessLibraryScanArgs{LibraryID: tt.lib.ID, Force: true}
			for _, requested := range [][]string{nil, {tt.lib.Paths[0]}} {
				args := processLibraryScanFanoutArgs(tt.lib, base, requested, tt.inventory)
				require.Len(t, args, len(tt.want))
				got := make([]string, 0, len(args))
				for _, arg := range args {
					require.Len(t, arg.ScopePaths, 1)
					require.NotEqual(t, tt.lib.Paths[0], arg.ScopePaths[0])
					got = append(got, arg.ScopePaths[0])
				}
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestProcessLibraryScanFanoutBatchesMusicBeforeArtistMetadata(t *testing.T) {
	lib := sqlc.Library{
		ID:        7,
		MediaType: sqlc.MediaTypeMusic,
		Paths:     []string{"/storage/Music"},
	}
	inv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
		Root: "/storage/Music",
		Files: []scanner.InventoryFile{
			{Root: "/storage/Music", Path: "/storage/Music/Alpha/First/01.flac", RelPath: "Alpha/First/01.flac", Class: scanner.ClassPrimaryMedia},
			{Root: "/storage/Music", Path: "/storage/Music/Beta/Second/01.flac", RelPath: "Beta/Second/01.flac", Class: scanner.ClassPrimaryMedia},
			{Root: "/storage/Music", Path: "/storage/Music/Gamma/Third/01.flac", RelPath: "Gamma/Third/01.flac", Class: scanner.ClassPrimaryMedia},
		},
	}}}
	base := ProcessLibraryScanArgs{LibraryID: lib.ID, Force: true}

	t.Run("full scan uses one whole-library job", func(t *testing.T) {
		args := processLibraryScanFanoutArgs(lib, base, []string{
			"/storage/Music/Alpha",
			"/storage/Music/Beta",
			"/storage/Music/Gamma",
		}, inv)

		require.Equal(t, []ProcessLibraryScanArgs{base}, args)
	})

	t.Run("changed artists share one scoped job", func(t *testing.T) {
		args := processLibraryScanFanoutArgs(lib, base, []string{
			"/storage/Music/Beta",
			"/storage/Music/Alpha",
			"/storage/Music/Alpha/First",
		}, inv)

		require.Len(t, args, 1)
		require.Equal(t, []string{
			"/storage/Music/Alpha",
			"/storage/Music/Beta",
		}, args[0].ScopePaths)
	})
}

func TestScannerRichMetadataTargetsAndDetail(t *testing.T) {
	detail := &metadata.MediaDetail{Title: "Dune"}
	result := scanner.Result{
		MovieApply: []scanner.MovieApplyResult{{
			Key:         "tmdb:438631",
			Action:      "applied",
			MediaItemID: 42,
		}, {
			Key:         "tmdb:999001",
			Action:      "skipped",
			MediaItemID: 43,
		}},
		MovieMetadata: []scanner.MovieFetchPreview{{
			Key:    "tmdb:438631",
			Detail: detail,
		}},
	}

	targets := scannerRichMetadataTargets(sqlc.Library{MediaType: sqlc.MediaTypeMovie}, result)
	require.Len(t, targets, 1)
	require.Equal(t, int64(42), targets[0].mediaItemID)
	require.Equal(t, metadata.KindMovie, targets[0].kind)

	got, kind, err := richMetadataDetailForJob(result, ApplyRichMetadataArgs{
		MediaKind: string(metadata.KindMovie),
		Key:       "tmdb:438631",
	})
	require.NoError(t, err)
	require.Equal(t, metadata.KindMovie, kind)
	require.Same(t, detail, got)
}

func TestLibraryScanProgressLabelIncludesScope(t *testing.T) {
	lib := sqlc.Library{Name: "Movies", Paths: []string{"/storage/Movies"}}

	require.Equal(t, "Movies", libraryScanProgressLabel(lib, nil))
	require.Equal(t, "Movies · The Matrix (1999)", libraryScanProgressLabel(lib, []string{"/storage/Movies/The Matrix (1999)"}))
	require.Equal(t, "Movies · The Matrix (1999) +1", libraryScanProgressLabel(lib, []string{
		"/storage/Movies/The Matrix (1999)",
		"/storage/Movies/Alien (1979)",
	}))
	require.Equal(t, "Movies · Loose Folder", libraryScanProgressLabel(lib, []string{"Loose Folder"}))
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
