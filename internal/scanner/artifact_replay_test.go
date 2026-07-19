package scanner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestValidateScannerAnalysisArtifactReplayRejectsChangedSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "track.flac")
	require.NoError(t, os.WriteFile(path, []byte("original bytes"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	data, err := marshalResultArtifact(scanArtifactKindAnalyze, Options{}, Result{
		Inventory: Inventory{Roots: []InventoryRoot{{Root: dir, Files: []InventoryFile{{
			Root: dir, Path: path, RelPath: "track.flac", Size: info.Size(), MTime: info.ModTime(), Class: ClassPrimaryMedia,
		}}}}},
	})
	require.NoError(t, err)
	artifact := sqlc.ScannerEntityArtifact{Stage: scanArtifactKindAnalyze, Data: data}
	require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact))

	require.NoError(t, os.WriteFile(path, []byte("replacement audio bytes"), 0o600))
	var stale *ArtifactReplayError
	require.ErrorAs(t, ValidateScannerAnalysisArtifactReplay(artifact), &stale)
	require.Equal(t, "source size changed", stale.Reason)
}

func TestGeneratedSidecarStaysInEntityArtifactAndHashInvalidatesEdit(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie (2026)")
	require.NoError(t, os.MkdirAll(movieDir, 0o755))
	mediaPath := filepath.Join(movieDir, "Movie (2026).mkv")
	nfoPath := filepath.Join(movieDir, "movie.nfo")
	require.NoError(t, os.WriteFile(mediaPath, []byte("video"), 0o600))
	nfoContent := []byte("generated-nfo")
	require.NoError(t, os.WriteFile(nfoPath, nfoContent, 0o600))
	mediaInfo, err := os.Stat(mediaPath)
	require.NoError(t, err)
	nfoInfo, err := os.Stat(nfoPath)
	require.NoError(t, err)
	digest := sha256.Sum256(nfoContent)
	key := "title_year:movie|2026"
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{Root: dir, Files: []InventoryFile{
			{Root: dir, Path: mediaPath, RelPath: "Movie (2026)/Movie (2026).mkv", Size: mediaInfo.Size(), MTime: mediaInfo.ModTime(), Class: ClassPrimaryMedia},
			{Root: dir, Path: nfoPath, RelPath: "Movie (2026)/movie.nfo", Size: nfoInfo.Size(), MTime: nfoInfo.ModTime(), Class: ClassNFO, Generated: true, SourceSHA256: hex.EncodeToString(digest[:])},
		}}}},
		MovieMatches: []MovieMatch{{Key: key, Files: []string{"Movie (2026)/Movie (2026).mkv"}}},
	}

	narrow := filterResultToIdentityKey(result, key)
	require.Len(t, narrow.Inventory.Roots, 1)
	require.Len(t, narrow.Inventory.Roots[0].Files, 2, "ignored generated sidecar must remain a replay source")
	data, err := marshalResultArtifact(scanArtifactKindAnalyze, Options{}, narrow)
	require.NoError(t, err)
	artifact := sqlc.ScannerEntityArtifact{Stage: scanArtifactKindAnalyze, Data: data}
	require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact))

	userEdit := []byte("user-edit-nfo")
	require.Len(t, userEdit, len(nfoContent))
	require.NoError(t, os.WriteFile(nfoPath, userEdit, 0o600))
	require.NoError(t, os.Chtimes(nfoPath, nfoInfo.ModTime(), nfoInfo.ModTime()))
	var stale *ArtifactReplayError
	require.ErrorAs(t, ValidateScannerAnalysisArtifactReplay(artifact), &stale)
	require.Equal(t, "generated source content changed", stale.Reason)
}

func TestOrdinaryUnparsedOwnerSidecarsStayInEntityArtifactAndHashInvalidateEdit(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "Movie (2026)")
	require.NoError(t, os.MkdirAll(movieDir, 0o755))
	mediaPath := filepath.Join(movieDir, "Movie (2026).mkv")
	nfoPath := filepath.Join(movieDir, "movie.nfo")
	plexmatchPath := filepath.Join(movieDir, ".plexmatch")
	artPath := filepath.Join(movieDir, "poster.jpg")
	require.NoError(t, os.WriteFile(mediaPath, []byte("video"), 0o600))
	require.NoError(t, os.WriteFile(nfoPath, []byte("<malformed"), 0o600))
	plexmatchContent := []byte("title: Movie")
	require.NoError(t, os.WriteFile(plexmatchPath, plexmatchContent, 0o600))
	require.NoError(t, os.WriteFile(artPath, []byte("user-art"), 0o600))

	files := make([]InventoryFile, 0, 4)
	for _, source := range []struct {
		path  string
		class FileClass
	}{
		{path: mediaPath, class: ClassPrimaryMedia},
		{path: nfoPath, class: ClassNFO},
		{path: plexmatchPath, class: ClassPlexmatch},
		{path: artPath, class: ClassArtwork},
	} {
		info, err := os.Stat(source.path)
		require.NoError(t, err)
		file := InventoryFile{
			Root:    dir,
			Path:    source.path,
			RelPath: filepath.ToSlash(mustRelPath(t, dir, source.path)),
			Size:    info.Size(),
			MTime:   info.ModTime(),
			Class:   source.class,
		}
		if isIdentityAffectingSidecar(file) {
			content, readErr := os.ReadFile(source.path) //nolint:gosec // test fixture in a temporary directory
			require.NoError(t, readErr)
			digest := sha256.Sum256(content)
			file.SourceSHA256 = hex.EncodeToString(digest[:])
		}
		files = append(files, file)
	}

	key := "title_year:movie|2026"
	result := Result{
		Inventory:    Inventory{Roots: []InventoryRoot{{Root: dir, Files: files}}},
		MovieMatches: []MovieMatch{{Key: key, Files: []string{"Movie (2026)/Movie (2026).mkv"}}},
	}
	narrow := filterResultToIdentityKey(result, key)
	require.Len(t, narrow.Inventory.Roots, 1)
	require.Len(t, narrow.Inventory.Roots[0].Files, 4, "unparsed owner sidecars must remain artifact replay sources")
	for _, file := range narrow.Inventory.Roots[0].Files {
		if isIdentityAffectingSidecar(file) {
			require.NotEmpty(t, file.SourceSHA256, file.RelPath)
			require.False(t, file.Generated, "ordinary user sidecar must not be mislabeled as generated")
		}
	}

	data, err := marshalResultArtifact(scanArtifactKindAnalyze, Options{}, narrow)
	require.NoError(t, err)
	artifact := sqlc.ScannerEntityArtifact{Stage: scanArtifactKindAnalyze, Data: data}
	require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact))

	plexmatchInfo, err := os.Stat(plexmatchPath)
	require.NoError(t, err)
	userEdit := []byte("title: Other")
	require.Len(t, userEdit, len(plexmatchContent))
	require.NoError(t, os.WriteFile(plexmatchPath, userEdit, 0o600))
	require.NoError(t, os.Chtimes(plexmatchPath, plexmatchInfo.ModTime(), plexmatchInfo.ModTime()))
	var stale *ArtifactReplayError
	require.ErrorAs(t, ValidateScannerAnalysisArtifactReplay(artifact), &stale)
	require.Equal(t, "sidecar source content changed", stale.Reason)
}

func mustRelPath(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	return rel
}

func TestValidateScannerAnalysisArtifactReplayRejectsOldPipelineRevision(t *testing.T) {
	artifact := sqlc.ScannerEntityArtifact{
		Stage: scanArtifactKindAnalyze,
		Data:  []byte(`{"schema_version":1,"inventory":{"roots":[]},"result":{}}`),
	}
	var stale *ArtifactReplayError
	require.ErrorAs(t, ValidateScannerAnalysisArtifactReplay(artifact), &stale)
	require.Contains(t, stale.Reason, "pipeline revision 0")
}

func TestArtifactSourceSetRejectsNewOwnerIdentitySources(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		file    string
		content string
	}{
		{name: "nfo", file: "movie.nfo", content: "<movie/>"},
		{name: "plexmatch", file: ".plexmatch", content: "title: Movie"},
		{name: "artwork", file: "poster.jpg", content: "image"},
		{name: "primary media", file: "Movie - Part 2.mkv", content: "video-two"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			owner := filepath.Join(root, "Movie (2026)")
			require.NoError(t, os.MkdirAll(owner, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(owner, "Movie (2026).mkv"), []byte("video"), 0o600))
			artifact := mustScopedSourceSetArtifact(t, root, owner)
			require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact))

			require.NoError(t, os.WriteFile(filepath.Join(owner, fixture.file), []byte(fixture.content), 0o600))
			var stale *ArtifactReplayError
			require.ErrorAs(t, ValidateScannerAnalysisArtifactReplay(artifact), &stale)
			require.Equal(t, "identity-relevant source set changed", stale.Reason)
		})
	}
}

func TestArtifactSourceSetIgnoresSiblingScopeAndHeyaInternalEntries(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "Movie A")
	sibling := filepath.Join(root, "Movie B")
	require.NoError(t, os.MkdirAll(owner, 0o755))
	require.NoError(t, os.MkdirAll(sibling, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(owner, "Movie A.mkv"), []byte("video-a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sibling, "Movie B.mkv"), []byte("video-b"), 0o600))
	artifact := mustScopedSourceSetArtifact(t, root, owner)
	require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact))

	require.NoError(t, os.WriteFile(filepath.Join(sibling, "movie.nfo"), []byte("<movie/>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(owner, ".heya-atomic-movie.nfo.crash.tmp"), []byte("partial"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(owner, ".heya-generated-31d7877c.previous"), []byte("predecessor"), 0o600))
	require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact), "a sibling owner and protocol-private entries are outside this owner source set")
}

func TestArtifactSourceSetAllowsProvenancedHeyaSidecarsButRejectsNewUserNFO(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	root := t.TempDir()
	owner := filepath.Join(root, "Movie")
	require.NoError(t, os.MkdirAll(owner, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(owner, "Movie.mkv"), []byte("video"), 0o600))

	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "artifact-generated-source-set", MediaType: sqlc.MediaTypeMovie,
		Paths: []string{root}, ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy: testutil.TestUserID(t, pool), Settings: []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	beforeGenerated := mustScopedSourceSetArtifact(t, root, owner)
	prepared, err := generatedwrite.PrepareBytes(filepath.Join(owner, "poster.jpg"), 0o644, []byte("heya-art"))
	require.NoError(t, err)
	_, outcome, err := generatedwrite.Publish(ctx, pool, nil, prepared)
	require.NoError(t, err)
	require.Equal(t, generatedwrite.OutcomePublished, outcome)
	require.NoError(t, ValidateScannerAnalysisArtifactReplayWithDB(ctx, pool, beforeGenerated), "a newly published Heya sidecar must not stale its own generation")

	inv, err := WalkInventoryScoped(ctx, []string{root}, []string{owner}, NewEventSink(Event{}))
	require.NoError(t, err)
	marked, err := markGeneratedSidecars(ctx, pool, lib.ID, &inv)
	require.NoError(t, err)
	require.Equal(t, 1, marked)
	data, err := marshalResultArtifact(scanArtifactKindAnalyze, Options{ScopePaths: []string{owner}}, Result{Inventory: inv})
	require.NoError(t, err)
	persistent := sqlc.ScannerEntityArtifact{Stage: scanArtifactKindAnalyze, Data: data}
	require.NoError(t, ValidateScannerAnalysisArtifactReplayWithDB(ctx, pool, persistent), "a generated sidecar present at analysis remains excluded on replay")

	require.NoError(t, os.WriteFile(filepath.Join(owner, "movie.nfo"), []byte("<movie><title>User title</title></movie>"), 0o600))
	var stale *ArtifactReplayError
	require.ErrorAs(t, ValidateScannerAnalysisArtifactReplayWithDB(ctx, pool, persistent), &stale)
	require.Equal(t, "identity-relevant source set changed", stale.Reason)
}

func TestArtifactSourceSetRejectsChangedUnclaimedPrimaryOutsideNarrowInventory(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "Owner")
	require.NoError(t, os.MkdirAll(owner, 0o755))
	claimedPath := filepath.Join(owner, "Claimed.mkv")
	unclaimedPath := filepath.Join(owner, "Unparsed.mkv")
	require.NoError(t, os.WriteFile(claimedPath, []byte("claimed"), 0o600))
	require.NoError(t, os.WriteFile(unclaimedPath, []byte("unclaimed"), 0o600))
	inv, err := WalkInventoryScoped(context.Background(), []string{root}, []string{owner}, NewEventSink(Event{}))
	require.NoError(t, err)
	key := "title:claimed"
	broad := Result{
		Inventory:         inv,
		MovieMatches:      []MovieMatch{{Key: key, Files: []string{filepath.ToSlash(filepath.Join("Owner", "Claimed.mkv"))}}},
		artifactSourceSet: sourceSetFromInventory(inv, []string{owner}),
	}
	narrow := filterResultToIdentityKey(broad, key)
	require.Len(t, narrow.Inventory.Roots, 1)
	require.Len(t, narrow.Inventory.Roots[0].Files, 1, "fixture must prove the changed file is absent from the narrow entity inventory")
	data, err := marshalResultArtifact(scanArtifactKindAnalyze, Options{ScopePaths: []string{owner}}, narrow)
	require.NoError(t, err)
	artifact := sqlc.ScannerEntityArtifact{Stage: scanArtifactKindAnalyze, Data: data}
	require.NoError(t, ValidateScannerAnalysisArtifactReplay(artifact))

	require.NoError(t, os.WriteFile(unclaimedPath, []byte("unclaimed-now-identifiable"), 0o600))
	var stale *ArtifactReplayError
	require.ErrorAs(t, ValidateScannerAnalysisArtifactReplay(artifact), &stale)
	require.Equal(t, "identity-relevant source set changed", stale.Reason)
}

func mustScopedSourceSetArtifact(t *testing.T, root, scope string) sqlc.ScannerEntityArtifact {
	t.Helper()
	inv, err := WalkInventoryScoped(context.Background(), []string{root}, []string{scope}, NewEventSink(Event{}))
	require.NoError(t, err)
	data, err := marshalResultArtifact(scanArtifactKindAnalyze, Options{ScopePaths: []string{scope}}, Result{Inventory: inv})
	require.NoError(t, err)
	return sqlc.ScannerEntityArtifact{Stage: scanArtifactKindAnalyze, Data: data}
}
