package scanner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPool *pgxpool.Pool

func setupPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testutil.SetupDB(t)
	testPool = pool
	return pool
}

func createTestLibrary(t *testing.T, q *sqlc.Queries, path string, mediaType sqlc.MediaType) sqlc.Library {
	t.Helper()
	lib, err := q.CreateLibrary(context.Background(), sqlc.CreateLibraryParams{
		Name:         t.Name() + "-" + string(mediaType),
		MediaType:    mediaType,
		Paths:        []string{path},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, testPool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	return lib
}

func createTestFile(t *testing.T, dir, relPath string, size int) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, make([]byte, size), 0o644))
}

func TestScanDiscoversMediaFiles(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	createTestFile(t, dir, "Movie B (2023)/Movie B (2023).mp4", 200)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 2, result.Discovered)
	assert.Equal(t, 2, result.New)
	assert.Equal(t, 0, result.Unchanged)
	assert.Equal(t, 0, result.Deleted)

	files, err := q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: lib.ID, Limit: 100, Offset: 0,
	})
	require.NoError(t, err)
	assert.Len(t, files, 2)
	for _, f := range files {
		assert.Equal(t, sqlc.FileStatusPending, f.Status)
	}
}

func TestRescanDetectsUnchangedFiles(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	createTestFile(t, dir, "Movie B (2023)/Movie B (2023).mp4", 200)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	result1, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)
	assert.Equal(t, 2, result1.New)

	result2, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 2, result2.Discovered)
	assert.Equal(t, 0, result2.New, "rescan should not create new entries")
	assert.Equal(t, 2, result2.Unchanged, "rescan should detect files as unchanged")
	assert.Equal(t, 0, result2.Deleted)
}

func TestRescanDetectsUpdatedFile(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "Movie A (2024)", "Movie A (2024).mkv")
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	_, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(filePath, make([]byte, 999), 0o644))

	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered)
	assert.Equal(t, 1, result.Updated+result.New, "modified file should be re-processed")
	assert.Equal(t, 0, result.Unchanged)
}

func TestScanDetectsDeletedFiles(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	createTestFile(t, dir, "Movie B (2023)/Movie B (2023).mp4", 200)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	_, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(filepath.Join(dir, "Movie B (2023)")))

	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered)
	assert.Equal(t, 1, result.Deleted, "removed file should be soft-deleted")

	deleted, err := q.ListDeletedLibraryFiles(ctx, sqlc.ListDeletedLibraryFilesParams{
		LibraryID: lib.ID, Limit: 100, Offset: 0,
	})
	require.NoError(t, err)
	assert.Len(t, deleted, 1)
}

func TestScanRestoresSoftDeletedFiles(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	_, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(filepath.Join(dir, "Movie A (2024)")))
	_, err = s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	deleted, _ := q.ListDeletedLibraryFiles(ctx, sqlc.ListDeletedLibraryFilesParams{
		LibraryID: lib.ID, Limit: 100, Offset: 0,
	})
	require.Len(t, deleted, 1, "file should be soft-deleted")

	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered)
	assert.Equal(t, 1, result.New, "restored file should be counted as new")

	deleted2, _ := q.ListDeletedLibraryFiles(ctx, sqlc.ListDeletedLibraryFilesParams{
		LibraryID: lib.ID, Limit: 100, Offset: 0,
	})
	assert.Len(t, deleted2, 0, "soft-deleted record should be restored")
}

func TestScanSkipsJunkFiles(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	createTestFile(t, dir, "Movie A (2024)/.DS_Store", 10)
	createTestFile(t, dir, "Movie A (2024)/Thumbs.db", 10)
	createTestFile(t, dir, "Movie A (2024)/movie.nfo", 50)
	createTestFile(t, dir, "Movie A (2024)/poster.jpg", 50)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered, "only the .mkv should be discovered")
}

func TestScanSkipsHiddenDirectories(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	createTestFile(t, dir, ".hidden/secret.mkv", 100)
	createTestFile(t, dir, "@eaDir/metadata.mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered, "hidden/system dirs should be skipped")
}

func TestScanSkipsExtrasDirs(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)
	createTestFile(t, dir, "Movie A (2024)/trailers/trailer.mkv", 50)
	createTestFile(t, dir, "Movie A (2024)/featurettes/making-of.mkv", 50)
	createTestFile(t, dir, "Movie A (2024)/behind the scenes/bts.mkv", 50)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered, "extras directories should be skipped")
}

func TestScanNFOAssociation(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	nfoContent := `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <title>Test Movie</title>
  <uniqueid type="tmdb">12345</uniqueid>
  <uniqueid type="imdb">tt0000001</uniqueid>
</movie>`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Test Movie (2024)"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Test Movie (2024)", "movie.nfo"), []byte(nfoContent), 0o644))
	createTestFile(t, dir, "Test Movie (2024)/Test Movie (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.New)

	files, err := q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: lib.ID, Limit: 10, Offset: 0,
	})
	require.NoError(t, err)
	require.Len(t, files, 1)
	pr := string(files[0].ParseResult)
	assert.Contains(t, pr, `"TMDBID"`)
	assert.Contains(t, pr, `12345`)
	assert.Contains(t, pr, `"IMDBID"`)
	assert.Contains(t, pr, `tt0000001`)
}

func TestForceRescanBypassesUnchangedCheck(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	_, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{ForceRescan: true})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered)
	assert.Equal(t, 1, result.New, "force rescan should re-process all files")
	assert.Equal(t, 0, result.Unchanged, "force rescan should not mark anything as unchanged")
}

func TestScanMultipleSeasons(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	nfoContent := `<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<tvshow>
  <title>Test Show</title>
  <uniqueid type="tmdb">99999</uniqueid>
</tvshow>`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Test Show (2020)"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Test Show (2020)", "tvshow.nfo"), []byte(nfoContent), 0o644))

	for _, ep := range []string{
		"Season 01/Test Show (2020) - S01E01.mkv",
		"Season 01/Test Show (2020) - S01E02.mkv",
		"Season 02/Test Show (2020) - S02E01.mkv",
	} {
		createTestFile(t, dir, filepath.Join("Test Show (2020)", ep), 100)
	}

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeTv)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 3, result.Discovered)
	assert.Equal(t, 3, result.New)

	result2, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 3, result2.Discovered)
	assert.Equal(t, 0, result2.New, "rescan of TV episodes should detect them as unchanged")
	assert.Equal(t, 3, result2.Unchanged)
}

func TestRescanAfterStatusChange(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	_, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	files, _ := q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: lib.ID, Limit: 10, Offset: 0,
	})
	require.Len(t, files, 1)
	q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID:     files[0].ID,
		Status: sqlc.FileStatusMatched,
	})

	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Unchanged, "matched files should stay unchanged on rescan")
	assert.Equal(t, 0, result.New, "matched files should not be re-enqueued")

	updated, _ := q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: lib.ID, Limit: 10, Offset: 0,
	})
	require.Len(t, updated, 1)
	assert.Equal(t, sqlc.FileStatusMatched, updated[0].Status, "status should remain matched after rescan")
}

func TestMtimePrecisionDoesNotCauseRescan(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	result1, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result1.New)

	for i := 0; i < 5; i++ {
		result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
		require.NoError(t, err)
		assert.Equal(t, 0, result.New, "scan %d should not create new entries", i+1)
		assert.Equal(t, 1, result.Unchanged, "scan %d should detect file as unchanged", i+1)
	}
}

func TestScanWithTestdata(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	movieDir := filepath.Join(testdataRoot(), "scanner", "movies")
	if _, err := os.Stat(movieDir); os.IsNotExist(err) {
		t.Skip("testdata/scanner/movies not found")
	}

	lib := createTestLibrary(t, q, movieDir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	result1, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, result1.Discovered, "should discover 3 movie files")
	assert.Equal(t, 3, result1.New)

	result2, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, result2.Discovered)
	assert.Equal(t, 0, result2.New, "rescan should not find new files")
	assert.Equal(t, 3, result2.Unchanged, "rescan should detect all 3 files as unchanged")

	files, err := q.ListLibraryFiles(ctx, sqlc.ListLibraryFilesParams{
		LibraryID: lib.ID, Limit: 100, Offset: 0,
	})
	require.NoError(t, err)
	for _, f := range files {
		assert.Contains(t, string(f.ParseResult), `"nfo"`, "each movie file should have NFO data: %s", f.Path)
	}
}

func TestScanTVWithTestdata(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	tvDir := filepath.Join(testdataRoot(), "scanner", "tv")
	if _, err := os.Stat(tvDir); os.IsNotExist(err) {
		t.Skip("testdata/scanner/tv not found")
	}

	lib := createTestLibrary(t, q, tvDir, sqlc.MediaTypeTv)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)

	result1, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)
	assert.Equal(t, 62, result1.Discovered, "should discover 62 Breaking Bad episodes")
	assert.Equal(t, 62, result1.New)

	result2, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)
	assert.Equal(t, 62, result2.Discovered)
	assert.Equal(t, 0, result2.New, "rescan should not find new files")
	assert.Equal(t, 62, result2.Unchanged, "rescan should detect all 62 episodes as unchanged")
}

func TestScanEmptyDirectory(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Discovered)
	assert.Equal(t, 0, result.New)
}

func TestScanNonMediaFilesIgnored(t *testing.T) {
	pool := setupPool(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	dir := t.TempDir()
	createTestFile(t, dir, "readme.txt", 100)
	createTestFile(t, dir, "notes.docx", 200)
	createTestFile(t, dir, "data.json", 50)
	createTestFile(t, dir, "Movie A (2024)/Movie A (2024).mkv", 100)

	lib := createTestLibrary(t, q, dir, sqlc.MediaTypeMovie)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	s := scanner.New(pool)
	result, err := s.ScanLibrary(ctx, lib, scanner.ScanOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Discovered, "only .mkv should be counted")
}

func testdataRoot() string {
	wd, _ := os.Getwd()
	for d := wd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "testdata")); err == nil {
			return filepath.Join(d, "testdata")
		}
	}
	return filepath.Join(wd, "..", "..", "testdata")
}
