package scanner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMarkGeneratedSidecarsRetainsSourcesButInvalidatesUserEdit(t *testing.T) {
	pool, library, root := generatedSidecarTestLibrary(t)
	ctx := context.Background()
	dir := filepath.Join(root, "Ado", "Kyougen")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	nfoPath := filepath.Join(dir, "album.nfo")
	artPath := filepath.Join(dir, "cover.jpg")
	nfoContent := []byte("<album><title>Kyougen</title></album>")
	artContent := []byte("generated-cover")
	require.NoError(t, os.WriteFile(nfoPath, nfoContent, 0o644))
	require.NoError(t, os.WriteFile(artPath, artContent, 0o644))
	insertGeneratedSidecarSignature(t, pool, library.ID, nfoPath, nfoContent)
	insertGeneratedSidecarSignature(t, pool, library.ID, artPath, artContent)

	inv, err := WalkInventory(ctx, []string{root}, NewEventSink(Event{}))
	require.NoError(t, err)
	beforeCount := countInventoryFiles(inv)
	marked, err := markGeneratedSidecars(ctx, pool, library.ID, &inv)
	require.NoError(t, err)
	require.Equal(t, 2, marked)
	require.Equal(t, beforeCount, countInventoryFiles(inv), "generated files remain artifact replay sources")
	require.True(t, inventoryFileAtPath(t, inv, nfoPath).Generated)
	require.True(t, inventoryFileAtPath(t, inv, artPath).Generated)

	// Preserve size and restore the generated mtime: SHA-256 still proves this
	// is a later user edit. An expired/no-pending publication is retired under
	// the path lock and the already-hashed bytes become user evidence now.
	nfoInfo, err := os.Stat(nfoPath)
	require.NoError(t, err)
	userContent := append([]byte(nil), nfoContent...)
	userContent[1] = 'u'
	require.NoError(t, os.WriteFile(nfoPath, userContent, 0o644))
	require.NoError(t, os.Chtimes(nfoPath, nfoInfo.ModTime(), nfoInfo.ModTime()))

	inv, err = WalkInventory(ctx, []string{root}, NewEventSink(Event{}))
	require.NoError(t, err)
	marked, err = markGeneratedSidecars(ctx, pool, library.ID, &inv)
	require.NoError(t, err)
	require.Equal(t, 1, marked)
	require.False(t, generatedSidecarRowExists(t, pool, library.ID, nfoPath))
	require.True(t, generatedSidecarRowExists(t, pool, library.ID, artPath))
	require.False(t, inventoryFileAtPath(t, inv, nfoPath).Generated, "user edit becomes ordinary matcher evidence in the stable pass")
	require.True(t, inventoryFileAtPath(t, inv, artPath).Generated)
}

func TestMarkGeneratedSidecarsCASMissPreservesConcurrentSaverUpsert(t *testing.T) {
	pool, library, root := generatedSidecarTestLibrary(t)
	ctx := context.Background()
	dir := filepath.Join(root, "Artist")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "artist.nfo")
	original := []byte("old generated")
	require.NoError(t, os.WriteFile(path, original, 0o644))
	insertGeneratedSidecarSignature(t, pool, library.ID, path, original)
	originalInfo, err := os.Stat(path)
	require.NoError(t, err)

	// Make the loaded row stale while preserving size and mtime so validation
	// has to compare the content hash.
	userEdit := []byte("usr generated")
	require.Len(t, userEdit, len(original))
	require.NoError(t, os.WriteFile(path, userEdit, 0o644))
	require.NoError(t, os.Chtimes(path, originalInfo.ModTime(), originalInfo.ModTime()))
	inv, err := WalkInventory(ctx, []string{root}, NewEventSink(Event{}))
	require.NoError(t, err)

	newGenerated := []byte("new generated")
	require.Len(t, newGenerated, len(original))
	_, err = markGeneratedSidecarsWithHook(ctx, pool, library.ID, &inv, func() {
		newMTime := originalInfo.ModTime().Add(2 * time.Second)
		require.NoError(t, os.WriteFile(path, newGenerated, 0o644))
		require.NoError(t, os.Chtimes(path, newMTime, newMTime))
		insertGeneratedSidecarSignature(t, pool, library.ID, path, newGenerated)
	})
	require.ErrorContains(t, err, "changed concurrently")
	require.True(t, generatedSidecarRowExists(t, pool, library.ID, path), "CAS miss must not erase the newer saver row")

	fresh, err := WalkInventory(ctx, []string{root}, NewEventSink(Event{}))
	require.NoError(t, err)
	marked, err := markGeneratedSidecars(ctx, pool, library.ID, &fresh)
	require.NoError(t, err)
	require.Equal(t, 1, marked)
	require.True(t, inventoryFileAtPath(t, fresh, path).Generated)
}

func TestMarkGeneratedSidecarsRehashesAfterPublicationLockWait(t *testing.T) {
	pool, library, root := generatedSidecarTestLibrary(t)
	ctx := context.Background()
	dir := filepath.Join(root, "Artist")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "artist.nfo")
	generated := []byte("old generated")
	require.NoError(t, os.WriteFile(path, generated, 0o644))
	insertGeneratedSidecarSignature(t, pool, library.ID, path, generated)
	info, err := os.Stat(path)
	require.NoError(t, err)
	inv, err := WalkInventory(ctx, []string{root}, NewEventSink(Event{}))
	require.NoError(t, err)

	userEdit := []byte("usr generated")
	require.Len(t, userEdit, len(generated))
	marked, err := markGeneratedSidecarsWithHooks(ctx, pool, library.ID, &inv, generatedSidecarHooks{
		beforePathLock: func() {
			// Model a same-size, preserved-mtime edit after the optimistic hash
			// but before the scanner obtains the publication lock.
			require.NoError(t, os.WriteFile(path, userEdit, 0o644))
			require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
		},
	})
	require.NoError(t, err)
	require.Zero(t, marked)
	file := inventoryFileAtPath(t, inv, path)
	require.False(t, file.Generated)
	wantDigest := sha256.Sum256(userEdit)
	require.Equal(t, fmt.Sprintf("%x", wantDigest), file.SourceSHA256)
	require.False(t, generatedSidecarRowExists(t, pool, library.ID, path), "stale generated provenance must be retired")
}

func TestGeneratedMovieNFOAndArtworkAreNotAnalysisEvidence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Correct Movie (2024)")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Correct Movie (2024).mkv"), []byte("video"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "movie.nfo"), []byte(`<?xml version="1.0"?><movie><title>Poisoned Title</title><year>1999</year></movie>`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("generated-art"), 0o644))

	inv, err := WalkInventory(context.Background(), []string{root}, NewEventSink(Event{}))
	require.NoError(t, err)
	for rootIndex := range inv.Roots {
		for fileIndex := range inv.Roots[rootIndex].Files {
			file := &inv.Roots[rootIndex].Files[fileIndex]
			if file.Class == ClassNFO || file.Class == ClassArtwork {
				file.Generated = true
			}
		}
	}
	plans, err := AnalyzeMovies(context.Background(), inv, NewEventSink(Event{}))
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, "Correct Movie", plans[0].Title)
	require.Equal(t, "2024", plans[0].Year)
	require.Equal(t, "filename", plans[0].Source)
	require.Empty(t, plans[0].Assets)
}

func generatedSidecarTestLibrary(t *testing.T) (*pgxpool.Pool, sqlc.Library, string) {
	t.Helper()
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	root := t.TempDir()
	library, err := sqlc.New(pool).CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "scanner-generated-sidecars",
		MediaType:    sqlc.MediaTypeMusic,
		Paths:        []string{root},
		ScanInterval: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })
	return pool, library, root
}

func insertGeneratedSidecarSignature(t *testing.T, pool *pgxpool.Pool, libraryID int64, path string, content []byte) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	digest := sha256.Sum256(content)
	canonical, err := generatedwrite.CanonicalPath(path)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO generated_sidecar_publications (
			path, published_size, published_mtime, published_sha256, published_at
		) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (path) DO UPDATE SET
			published_size = EXCLUDED.published_size,
			published_mtime = EXCLUDED.published_mtime,
			published_sha256 = EXCLUDED.published_sha256,
			published_at = now(), updated_at = now()
	`, canonical, info.Size(), info.ModTime().Truncate(time.Microsecond), digest[:])
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO library_generated_sidecars (library_id, path)
		VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, libraryID, canonical)
	require.NoError(t, err)
}

func generatedSidecarRowExists(t *testing.T, pool *pgxpool.Pool, libraryID int64, path string) bool {
	t.Helper()
	var exists bool
	canonical, err := generatedwrite.CanonicalPath(path)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT EXISTS (SELECT 1 FROM library_generated_sidecars WHERE library_id = $1 AND path = $2)
	`, libraryID, canonical).Scan(&exists))
	return exists
}

func inventoryFileAtPath(t *testing.T, inv Inventory, path string) InventoryFile {
	t.Helper()
	for _, root := range inv.Roots {
		for _, file := range root.Files {
			if filepath.Clean(file.Path) == filepath.Clean(path) {
				return file
			}
		}
	}
	t.Fatalf("inventory file not found: %s", path)
	return InventoryFile{}
}
