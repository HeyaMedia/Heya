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
