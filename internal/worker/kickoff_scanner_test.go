package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/stretchr/testify/require"
)

func TestScannerWorkerErrorSnoozesDeferredMetadataWork(t *testing.T) {
	err := scannerWorkerError(&metadata.DeferredWorkError{Operation: "test discovery", RetryAfter: 30 * time.Second})
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, err, &snooze)
	require.Equal(t, 30*time.Second, snooze.Duration)
}

// The DB round-trips mtimes at Postgres's µs precision while a fresh
// os.Stat carries nanoseconds — the comparison must truncate both sides or
// every file with sub-µs mtime residue reads as changed on every scan,
// silently degrading incremental scans into full reprocesses.
func TestLibraryFileChangedTruncatesMtimeToMicroseconds(t *testing.T) {
	statMtime := time.Date(2026, 7, 10, 4, 0, 0, 123456789, time.UTC) // ns residue
	dbMtime := statMtime.Truncate(time.Microsecond)                   // what PG returns

	row := sqlc.ListLibraryFilesForScanRow{
		Size:  42,
		Mtime: pgtype.Timestamptz{Time: dbMtime, Valid: true},
	}
	file := scanner.InventoryFile{Size: 42, MTime: statMtime}
	require.False(t, libraryFileChanged(row, file), "µs-truncated equal mtimes must read as unchanged")

	file.MTime = statMtime.Add(2 * time.Second)
	require.True(t, libraryFileChanged(row, file), "a real mtime change must still be detected")

	file.MTime = statMtime
	file.Size = 43
	require.True(t, libraryFileChanged(row, file), "a size change must still be detected")
}

func TestTimestamptzChangedTruncatesToMicroseconds(t *testing.T) {
	statMtime := time.Date(2026, 7, 10, 4, 0, 0, 999999999, time.UTC)
	dbMtime := statMtime.Truncate(time.Microsecond)

	a := pgtype.Timestamptz{Time: dbMtime, Valid: true}
	b := pgtype.Timestamptz{Time: statMtime, Valid: true}
	require.False(t, timestamptzChanged(a, b), "µs-truncated equal timestamps must read as unchanged")

	b.Time = statMtime.Add(time.Millisecond)
	require.True(t, timestamptzChanged(a, b), "a >1µs difference must still be detected")

	require.True(t, timestamptzChanged(a, pgtype.Timestamptz{}), "validity mismatch must read as changed")
}

func TestMatchMovedFilesPairsBySizePlusBasenameOrMtime(t *testing.T) {
	mtime := time.Date(2026, 7, 10, 4, 0, 0, 123456789, time.UTC)
	rows := []sqlc.ListLibraryFilesForScanRow{
		{ID: 1, Path: "/media/Movies/Old Name (1999)/movie.mkv", Size: 100, Mtime: pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true}},
		{ID: 2, Path: "/media/Movies/Kept (2001)/kept.mkv", Size: 100, Mtime: pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true}},
		{ID: 3, Path: "/media/Movies/Renamed (2002)/before.mkv", Size: 300, Mtime: pgtype.Timestamptz{Time: mtime.Truncate(time.Microsecond), Valid: true}},
	}
	seen := map[string]bool{"/media/Movies/Kept (2001)/kept.mkv": true} // still on disk — never a candidate

	moves := matchMovedFiles(rows, seen, []scanner.InventoryFile{
		// moved across dirs: same size + same basename, mtime irrelevant
		{Path: "/media/Movies/New Name (1999)/movie.mkv", Size: 100, MTime: mtime.Add(time.Hour)},
		// renamed in place: same size + same µs-mtime, basename differs
		{Path: "/media/Movies/Renamed (2002)/after.mkv", Size: 300, MTime: mtime},
		// same size as row 1 but different basename AND mtime: no claim
		{Path: "/media/Movies/Impostor (2020)/impostor.mkv", Size: 100, MTime: mtime.Add(48 * time.Hour)},
	})

	require.Len(t, moves, 2)
	byID := map[int64]string{}
	for _, m := range moves {
		byID[m.Row.ID] = m.File.Path
	}
	require.Equal(t, "/media/Movies/New Name (1999)/movie.mkv", byID[1])
	require.Equal(t, "/media/Movies/Renamed (2002)/after.mkv", byID[3])
}

func TestMatchMovedFilesNeverClaimsBySizeAlone(t *testing.T) {
	rows := []sqlc.ListLibraryFilesForScanRow{
		{ID: 1, Path: "/media/Movies/Gone (1999)/gone.mkv", Size: 100},
	}
	moves := matchMovedFiles(rows, map[string]bool{}, []scanner.InventoryFile{
		{Path: "/media/Movies/Fresh (2024)/fresh.mkv", Size: 100, MTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	require.Empty(t, moves, "size alone must not transfer a row's identity")
}

func TestMatchMovedFilesSkipsStaleSoftDeletes(t *testing.T) {
	old := pgtype.Timestamptz{Time: time.Now().Add(-8 * 24 * time.Hour), Valid: true}
	recent := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
	rows := []sqlc.ListLibraryFilesForScanRow{
		{ID: 1, Path: "/media/Movies/Stale (1999)/movie.mkv", Size: 100, DeletedAt: old},
		{ID: 2, Path: "/media/Movies/Recent (2001)/movie.mkv", Size: 100, DeletedAt: recent},
	}
	moves := matchMovedFiles(rows, map[string]bool{}, []scanner.InventoryFile{
		{Path: "/media/Movies/Moved (2001)/movie.mkv", Size: 100},
	})
	require.Len(t, moves, 1)
	require.Equal(t, int64(2), moves[0].Row.ID, "stale soft-deletes are out of the 7-day window; the recent one wins")
}

func TestRelocateMovedFilesKeepsRowIDAndEscapesSoftDelete(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "kickoff-move-detection-test",
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/movies"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	mtime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	orig, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID:   lib.ID,
		Path:        "/media/movies/Old Title (1999)/Old Title.mkv",
		Size:        100,
		Mtime:       pgtype.Timestamptz{Time: mtime, Valid: true},
		ParseResult: []byte("{}"),
		Status:      sqlc.FileStatusMatched,
	})
	require.NoError(t, err)

	rows, err := q.ListLibraryFilesForScan(ctx, lib.ID)
	require.NoError(t, err)

	w := &KickoffLibraryScanWorker{DB: pool}
	seen := map[string]bool{}
	var scopes []string
	moved := w.relocateMovedFiles(ctx, q, lib, rows, seen, []scanner.InventoryFile{
		{Path: "/media/movies/New Title (1999)/Old Title.mkv", RelPath: "New Title (1999)/Old Title.mkv", Size: 100, MTime: mtime},
	}, func(scope string) { scopes = append(scopes, scope) })

	require.Equal(t, 1, moved)
	require.True(t, seen["/media/movies/Old Title (1999)/Old Title.mkv"], "old path must escape the soft-delete pass")
	require.Contains(t, scopes, "/media/movies/Old Title (1999)", "old owner scope re-enters the pipeline")

	row, err := q.GetLibraryFileByID(ctx, orig.ID)
	require.NoError(t, err)
	require.Equal(t, "/media/movies/New Title (1999)/Old Title.mkv", row.Path, "row keeps its id under the new path")
	require.False(t, row.DeletedAt.Valid)
}

// Compaction deletes ALL of an entity's artifacts, so the guard must key on
// the entity (not a single metadata_artifact_id, which let a newer apply
// cycle's compaction delete an older cycle's still-referenced artifact) and
// cover every pipeline kind that could still produce or consume a rich job —
// a live fetch/apply cycle will enqueue one we haven't seen yet.
func TestActiveScannerJobsForEntityGuardsByEntity(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()

	const entityWithJob, otherEntity int64 = 991001, 991002

	insertJob := func(kind string) int64 {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO river_job (kind, queue, args, max_attempts, state)
			VALUES ($1, $1, $2, 5, 'available')
			RETURNING id`, kind, []byte(`{"scanner_entity_id": 991001}`)).Scan(&id)
		require.NoError(t, err)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, id) })
		return id
	}

	// Each pipeline kind that can still lead to a rich job must block compaction,
	// including a mid-flight apply cycle that hasn't enqueued its rich job yet.
	for _, kind := range []string{"search_metadata", "fetch_metadata", "apply_metadata", "apply_rich_metadata"} {
		jobID := insertJob(kind)

		busy, err := activeScannerJobsForEntity(ctx, pool, entityWithJob, 0)
		require.NoError(t, err)
		require.True(t, busy, "a pending %s for the entity must block compaction", kind)

		busy, err = activeScannerJobsForEntity(ctx, pool, entityWithJob, jobID)
		require.NoError(t, err)
		require.False(t, busy, "the compacting job excludes itself (%s)", kind)

		busy, err = activeScannerJobsForEntity(ctx, pool, otherEntity, 0)
		require.NoError(t, err)
		require.False(t, busy, "an unrelated entity is not blocked (%s)", kind)

		_, err = pool.Exec(ctx, `DELETE FROM river_job WHERE id = $1`, jobID)
		require.NoError(t, err)
	}
}

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

func TestScannerPipelineQueuesArePartitionedByMediaType(t *testing.T) {
	tests := []struct {
		name      string
		mediaType sqlc.MediaType
		wantQueue string
	}{
		{name: "movies", mediaType: sqlc.MediaTypeMovie, wantQueue: "process_scan_movie"},
		{name: "tv", mediaType: sqlc.MediaTypeTv, wantQueue: "process_scan_tv"},
		{name: "anime", mediaType: sqlc.MediaTypeAnime, wantQueue: "process_scan_anime"},
		{name: "music", mediaType: sqlc.MediaTypeMusic, wantQueue: "process_scan_music"},
		{name: "books", mediaType: sqlc.MediaTypeBook, wantQueue: "process_scan_book"},
		{name: "unknown fallback", mediaType: sqlc.MediaType("future"), wantQueue: "process_scan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantQueue, ProcessLibraryScanArgs{MediaType: tt.mediaType}.InsertOpts().Queue)
		})
	}

	require.Equal(t, "kickoff_library_scan_anime", KickoffLibraryScanArgs{MediaType: sqlc.MediaTypeAnime}.InsertOpts().Queue)
	require.Equal(t, "search_metadata_music", SearchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic}.InsertOpts().Queue)
	require.Equal(t, "search_metadata_poll_music", SearchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic, Poll: true}.InsertOpts().Queue)
	require.Equal(t, "fetch_metadata_music", FetchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic}.InsertOpts().Queue)
	require.Equal(t, "fetch_metadata_poll_music", FetchLibraryMetadataArgs{MediaType: sqlc.MediaTypeMusic, Poll: true}.InsertOpts().Queue)
	require.Equal(t, "apply_metadata_tv", ApplyLibraryScanArgs{MediaType: sqlc.MediaTypeTv}.InsertOpts().Queue)
}

func TestRemoteMetadataPollContinuationsAreScheduledOnPollQueues(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)

	require.NoError(t, enqueueSearchLibraryMetadataAfter(ctx, rc, SearchLibraryMetadataArgs{
		LibraryID: 901, MediaType: sqlc.MediaTypeAnime, ScannerEntityID: 902, AnalysisArtifactID: 903, Poll: true,
	}, PriorityScan, "", time.Minute))
	require.NoError(t, enqueueFetchLibraryMetadataAfter(ctx, rc, FetchLibraryMetadataArgs{
		LibraryID: 904, MediaType: sqlc.MediaTypeMusic, ScannerEntityID: 905, SearchArtifactID: 906, Poll: true,
	}, PriorityScan, "", time.Minute))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE NULLIF(args->>'library_id', '')::bigint IN (901, 904)`)
	})

	rows, err := pool.Query(ctx, `
		SELECT kind, queue, state
		FROM river_job
		WHERE NULLIF(args->>'library_id', '')::bigint IN (901, 904)
		ORDER BY kind`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var kind, queue, state string
		require.NoError(t, rows.Scan(&kind, &queue, &state))
		got[kind] = [2]string{queue, state}
	}
	require.NoError(t, rows.Err())
	require.Equal(t, [2]string{"search_metadata_poll_anime", "scheduled"}, got["search_metadata"])
	require.Equal(t, [2]string{"fetch_metadata_poll_music", "scheduled"}, got["fetch_metadata"])
}

func TestRenameLegacyScannerJobsRoutesActiveBacklogByMediaType(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	userID := testutil.TestUserID(t, pool)

	createLibrary := func(name string, mediaType sqlc.MediaType) sqlc.Library {
		lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
			Name:         name,
			MediaType:    mediaType,
			Paths:        []string{"/media/" + name},
			ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
			CreatedBy:    userID,
			Settings:     []byte("{}"),
		})
		require.NoError(t, err)
		t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
		return lib
	}

	music := createLibrary("typed-queue-music", sqlc.MediaTypeMusic)
	anime := createLibrary("typed-queue-anime", sqlc.MediaTypeAnime)
	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)

	musicJob, err := rc.Insert(ctx, ProcessLibraryScanArgs{
		LibraryID:  music.ID,
		ScopePaths: []string{"/media/typed-queue-music/Artist"},
	}, nil)
	require.NoError(t, err)
	animeJob, err := rc.Insert(ctx, ProcessLibraryScanArgs{
		LibraryID:  anime.ID,
		ScopePaths: []string{"/media/typed-queue-anime/Series"},
	}, nil)
	require.NoError(t, err)
	musicPollJob, err := rc.Insert(ctx, FetchLibraryMetadataArgs{
		LibraryID:        music.ID,
		ScannerEntityID:  991,
		SearchArtifactID: 992,
		Poll:             true,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE id = ANY($1::bigint[])`, []int64{musicJob.Job.ID, animeJob.Job.ID, musicPollJob.Job.ID})
	})

	queueFor := func(jobID int64) string {
		var queue string
		require.NoError(t, pool.QueryRow(ctx, `SELECT queue FROM river_job WHERE id = $1`, jobID).Scan(&queue))
		return queue
	}
	require.Equal(t, "process_scan", queueFor(musicJob.Job.ID))
	require.Equal(t, "process_scan", queueFor(animeJob.Job.ID))
	require.Equal(t, "fetch_metadata_poll", queueFor(musicPollJob.Job.ID))

	require.NoError(t, renameLegacyScannerJobs(ctx, pool))
	require.Equal(t, "process_scan_music", queueFor(musicJob.Job.ID))
	require.Equal(t, "process_scan_anime", queueFor(animeJob.Job.ID))
	require.Equal(t, "fetch_metadata_poll_music", queueFor(musicPollJob.Job.ID))
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

// The scanner is a dumb per-owner-unit enqueuer: one process_scan job per
// artist / author / movie / show directory (or loose file). Grouping smarter
// than the directory structure — and all searching — happens downstream in
// the identify job, which skips the live search for already-known units via
// the persisted decisions overlay.
func TestProcessLibraryScanFanoutIsPerOwnerUnit(t *testing.T) {
	music := sqlc.Library{ID: 7, MediaType: sqlc.MediaTypeMusic, Paths: []string{"/storage/Music"}}
	musicInv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
		Root: "/storage/Music",
		Files: []scanner.InventoryFile{
			{Root: "/storage/Music", Path: "/storage/Music/Alpha/First/01.flac", RelPath: "Alpha/First/01.flac", Class: scanner.ClassPrimaryMedia},
			{Root: "/storage/Music", Path: "/storage/Music/Beta/Second/01.flac", RelPath: "Beta/Second/01.flac", Class: scanner.ClassPrimaryMedia},
			{Root: "/storage/Music", Path: "/storage/Music/Gamma/Third/01.flac", RelPath: "Gamma/Third/01.flac", Class: scanner.ClassPrimaryMedia},
		},
	}}}
	base := ProcessLibraryScanArgs{LibraryID: music.ID, Force: true}

	t.Run("music fans out one job per artist", func(t *testing.T) {
		for _, requested := range [][]string{nil, {"/storage/Music"}, {"/storage/Music/Alpha", "/storage/Music/Beta", "/storage/Music/Gamma"}} {
			args := processLibraryScanFanoutArgs(music, base, requested, musicInv)
			require.Len(t, args, 3)
			for i, want := range []string{"/storage/Music/Alpha", "/storage/Music/Beta", "/storage/Music/Gamma"} {
				require.Equal(t, []string{want}, args[i].ScopePaths)
			}
		}
	})

	t.Run("changed artist album collapses into its artist unit", func(t *testing.T) {
		args := processLibraryScanFanoutArgs(music, base, []string{
			"/storage/Music/Alpha",
			"/storage/Music/Alpha/First",
		}, musicInv)
		require.Len(t, args, 1)
		require.Equal(t, []string{"/storage/Music/Alpha"}, args[0].ScopePaths)
	})

	t.Run("books fan out per author directory", func(t *testing.T) {
		books := sqlc.Library{ID: 8, MediaType: sqlc.MediaTypeBook, Paths: []string{"/storage/Books"}}
		bookInv := scanner.Inventory{Roots: []scanner.InventoryRoot{{
			Root: "/storage/Books",
			Files: []scanner.InventoryFile{
				{Root: "/storage/Books", Path: "/storage/Books/Frank Herbert/Dune (1965)/Dune.epub", RelPath: "Frank Herbert/Dune (1965)/Dune.epub", Class: scanner.ClassPrimaryMedia},
				{Root: "/storage/Books", Path: "/storage/Books/Frank Herbert/Dune Messiah (1969)/Dune Messiah.epub", RelPath: "Frank Herbert/Dune Messiah (1969)/Dune Messiah.epub", Class: scanner.ClassPrimaryMedia},
				{Root: "/storage/Books", Path: "/storage/Books/Andy Weir - Project Hail Mary (2021).epub", RelPath: "Andy Weir - Project Hail Mary (2021).epub", Class: scanner.ClassPrimaryMedia},
			},
		}}}
		args := processLibraryScanFanoutArgs(books, ProcessLibraryScanArgs{LibraryID: books.ID}, nil, bookInv)
		require.Len(t, args, 2)
		require.Equal(t, []string{"/storage/Books/Andy Weir - Project Hail Mary (2021).epub"}, args[0].ScopePaths, "a loose file at the root is its own unit")
		require.Equal(t, []string{"/storage/Books/Frank Herbert"}, args[1].ScopePaths, "both Dune books share the author unit")
	})

	t.Run("empty library produces no jobs", func(t *testing.T) {
		args := processLibraryScanFanoutArgs(music, base, nil, scanner.Inventory{})
		require.Empty(t, args)
	})
}

func TestProcessLibraryScanNeedsOwnerFanout(t *testing.T) {
	lib := sqlc.Library{ID: 7, MediaType: sqlc.MediaTypeMusic, Paths: []string{"/storage/Music"}}

	require.True(t, processLibraryScanNeedsOwnerFanout(lib, nil), "nil-scope jobs re-fan into owner units")
	require.True(t, processLibraryScanNeedsOwnerFanout(lib, []string{"/storage/Music"}), "library-root scopes re-fan into owner units")
	require.True(t, processLibraryScanNeedsOwnerFanout(lib, []string{"/storage/Music/Alpha", "/storage/Music/Beta"}), "legacy multi-owner batches split")
	require.False(t, processLibraryScanNeedsOwnerFanout(lib, []string{"/storage/Music/Alpha"}), "a single owner unit runs as-is")

	require.True(t, scannerScopesNeedInventoryExpansion(lib, nil), "whole-library re-fanout needs the inventory")
	require.True(t, scannerScopesNeedInventoryExpansion(lib, []string{"/storage/Music"}), "root expansion needs the inventory")
	require.False(t, scannerScopesNeedInventoryExpansion(lib, []string{"/storage/Music/Alpha", "/storage/Music/Beta"}), "plain splits skip the walk")
}

func TestOrphanedScannerRequeueArgsSplitPerOwnerScope(t *testing.T) {
	args := orphanedScannerRequeueArgs([]orphanedScannerEntity{
		{ID: 1, LibraryID: 5, ScopePaths: []string{"/storage/Music/Alpha", "/storage/Music/Beta"}},
		{ID: 2, LibraryID: 5, ScopePaths: []string{"/storage/Music/Beta"}},
		{ID: 3, LibraryID: 5, ScopePaths: nil},
	})

	require.Len(t, args, 3, "per-scope splits dedupe across entities; nil-scope requeues once")
	require.Equal(t, []string{"/storage/Music/Alpha"}, args[0].ScopePaths)
	require.Equal(t, []string{"/storage/Music/Beta"}, args[1].ScopePaths)
	require.Nil(t, args[2].ScopePaths, "legacy whole-library entities requeue as nil-scope for worker re-fanout")
	for _, a := range args {
		require.True(t, a.Force, "requeues bypass change detection")
		require.EqualValues(t, 5, a.LibraryID)
	}
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

// The scan-progress denominator lives in library_scan_bursts, maintained
// transactionally with each unit insert: a unit enqueued while the library
// is idle RESETS the row (a new burst); while other units are active it
// increments. The bursts row is locked FOR UPDATE, so concurrent first
// units serialize — exactly one resets, the rest increment.
func TestInsertScanUnitWithBurstResetsWhenIdleIncrementsWhenActive(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	userID := testutil.TestUserID(t, pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scan-burst-bump-test",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{"/media/music"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID,
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = 'process_scan' AND NULLIF(args->>'library_id','')::bigint = $1`, lib.ID)
	})

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	require.NoError(t, err)

	burstTotal := func() int64 {
		var n int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT units_total FROM library_scan_bursts WHERE library_id = $1`, lib.ID).Scan(&n))
		return n
	}

	// Seed a stale row from a "previous burst".
	_, err = pool.Exec(ctx, `INSERT INTO library_scan_bursts (library_id, units_total) VALUES ($1, 9000)`, lib.ID)
	require.NoError(t, err)

	// First unit of a new burst: library idle → reset (the row lock plus
	// pre-insert idle check make this exact, no self-exclusion needed).
	require.NoError(t, EnqueueProcessLibraryScan(ctx, rc, pool, ProcessLibraryScanArgs{
		LibraryID:  lib.ID,
		ScopePaths: []string{"/media/music/Alpha"},
	}, PriorityScan, ""))
	require.EqualValues(t, 1, burstTotal(), "first unit of a burst resets the stale total")
	var firstQueue string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT queue FROM river_job
		WHERE kind = 'process_scan'
		  AND NULLIF(args->>'library_id', '')::bigint = $1
		ORDER BY id
		LIMIT 1`, lib.ID).Scan(&firstQueue))
	require.Equal(t, "process_scan_music", firstQueue)

	// Second unit while the first is queued → increment.
	require.NoError(t, EnqueueProcessLibraryScan(ctx, rc, pool, ProcessLibraryScanArgs{
		LibraryID:  lib.ID,
		ScopePaths: []string{"/media/music/Beta"},
	}, PriorityScan, ""))
	require.EqualValues(t, 2, burstTotal(), "subsequent units increment")

	// A dedup'd duplicate insert must not bump the counter.
	require.NoError(t, EnqueueProcessLibraryScan(ctx, rc, pool, ProcessLibraryScanArgs{
		LibraryID:  lib.ID,
		ScopePaths: []string{"/media/music/Beta"},
	}, PriorityScan, ""))
	require.EqualValues(t, 2, burstTotal(), "unique-dedup'd inserts leave the counter untouched")
}
