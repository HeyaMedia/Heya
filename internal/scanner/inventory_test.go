package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWalkInventoryScopedPreservesLibraryRelativePaths(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Show (2024)", "Season 01"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "Other (2024)", "Season 01"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Show (2024)", "tvshow.nfo"), []byte("<tvshow/>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Show (2024)", "Season 01", "Show.S01E01.mkv"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "Other (2024)", "Season 01", "Other.S01E01.mkv"), []byte("x"), 0o644))

	inv, err := WalkInventoryScoped(context.Background(), []string{root}, []string{filepath.Join(root, "Show (2024)")}, NewEventSink(Event{}))
	require.NoError(t, err)
	require.Len(t, inv.Roots, 1)

	var rels []string
	for _, file := range inv.Roots[0].Files {
		rels = append(rels, file.RelPath)
	}
	require.ElementsMatch(t, []string{
		filepath.Join("Show (2024)", "tvshow.nfo"),
		filepath.Join("Show (2024)", "Season 01", "Show.S01E01.mkv"),
	}, rels)
}

func TestWalkInventoryScopedKeepsDottedSceneDirectoryScope(t *testing.T) {
	root := t.TempDir()
	sceneDir := filepath.Join(root, "Anora.2024.1080p.BluRay.x264-PiGNUS")
	otherDir := filepath.Join(root, "Kill Bill Vol. 1 (2003)")
	require.NoError(t, os.MkdirAll(sceneDir, 0o755))
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sceneDir, "Anora.2024.1080p.BluRay.x264-PiGNUS.mkv"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Kill.Bill.Vol.1.2003.1080p.BluRay.x264-GRP-CD1.mkv"), []byte("x"), 0o644))

	inv, err := WalkInventoryScoped(context.Background(), []string{root}, []string{sceneDir}, NewEventSink(Event{}))
	require.NoError(t, err)
	require.Len(t, inv.Roots, 1)

	var rels []string
	for _, file := range inv.Roots[0].Files {
		rels = append(rels, file.RelPath)
	}
	require.ElementsMatch(t, []string{
		filepath.Join("Anora.2024.1080p.BluRay.x264-PiGNUS", "Anora.2024.1080p.BluRay.x264-PiGNUS.mkv"),
	}, rels)
}

func TestWalkInventoryScopedMediaFilePathScopesToFile(t *testing.T) {
	root := t.TempDir()
	sceneDir := filepath.Join(root, "Anora.2024.1080p.BluRay.x264-PiGNUS")
	otherDir := filepath.Join(root, "Kill Bill Vol. 1 (2003)")
	mediaPath := filepath.Join(sceneDir, "Anora.2024.1080p.BluRay.x264-PiGNUS.mkv")
	require.NoError(t, os.MkdirAll(sceneDir, 0o755))
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	require.NoError(t, os.WriteFile(mediaPath, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sceneDir, "poster.jpg"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(otherDir, "Kill.Bill.Vol.1.2003.1080p.BluRay.x264-GRP-CD1.mkv"), []byte("x"), 0o644))

	inv, err := WalkInventoryScoped(context.Background(), []string{root}, []string{mediaPath}, NewEventSink(Event{}))
	require.NoError(t, err)
	require.Len(t, inv.Roots, 1)

	var rels []string
	for _, file := range inv.Roots[0].Files {
		rels = append(rels, file.RelPath)
	}
	require.ElementsMatch(t, []string{
		filepath.Join("Anora.2024.1080p.BluRay.x264-PiGNUS", "Anora.2024.1080p.BluRay.x264-PiGNUS.mkv"),
	}, rels)
}
