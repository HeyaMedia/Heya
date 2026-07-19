package worker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/require"
)

func TestSaveNFOWorkerSkipsQueuedWriteAfterSettingDisabled(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)

	root := t.TempDir()
	mediaDir := filepath.Join(root, "Queued Movie")
	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "queued-nfo-setting-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_nfo":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })

	item, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, Title: "Queued Movie", SortTitle: "Queued Movie",
		ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	filePath := filepath.Join(mediaDir, "movie.mkv")
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: library.ID, Path: filePath, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID: file.ID, Status: sqlc.FileStatusMatched, MediaItemID: pgtype.Int8{Int64: item.ID, Valid: true},
	}))
	_, err = q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       library.ID,
		Settings: []byte(`{"save_nfo":false}`),
	})
	require.NoError(t, err)

	generatedWrites := &recordingGeneratedWriteSuppressor{}
	w := &SaveNFOWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil), GeneratedWrites: generatedWrites}
	job := &river.Job[SaveNFOArgs]{
		JobRow: &rivertype.JobRow{},
		Args: SaveNFOArgs{
			MediaItemID:   item.ID,
			LibraryFileID: file.ID,
			FilePath:      filePath,
			MediaType:     string(sqlc.MediaTypeMovie),
		},
	}
	require.NoError(t, w.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(mediaDir, "movie.nfo"))
	require.Empty(t, generatedWrites.outputs, "disabled queued job must not publish provenance")
}

func TestSaveNFOWorkerSkipsQueuedWriteAfterFileReassigned(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	root := t.TempDir()
	mediaDir := filepath.Join(root, "Old Movie")
	filePath := filepath.Join(mediaDir, "movie.mkv")
	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "queued-nfo-owner-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_nfo":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })
	oldItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, Title: "Old Movie", SortTitle: "Old Movie", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	newItem, err := q.CreateMediaItem(ctx, sqlc.CreateMediaItemParams{
		LibraryID: library.ID, MediaType: sqlc.MediaTypeMovie, Title: "New Movie", SortTitle: "New Movie", ExternalIds: []byte("{}"),
	})
	require.NoError(t, err)
	file, err := q.UpsertLibraryFile(ctx, sqlc.UpsertLibraryFileParams{
		LibraryID: library.ID, Path: filePath, ParseResult: []byte("{}"), Status: sqlc.FileStatusMatched,
	})
	require.NoError(t, err)
	require.NoError(t, q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
		ID: file.ID, Status: sqlc.FileStatusMatched, MediaItemID: pgtype.Int8{Int64: newItem.ID, Valid: true},
	}))

	generatedWrites := &recordingGeneratedWriteSuppressor{}
	w := &SaveNFOWorker{DB: pool, Progress: NewTaskProgressBroadcaster(nil), GeneratedWrites: generatedWrites}
	job := &river.Job[SaveNFOArgs]{JobRow: &rivertype.JobRow{}, Args: SaveNFOArgs{
		MediaItemID: oldItem.ID, LibraryFileID: file.ID, FilePath: filePath, MediaType: string(sqlc.MediaTypeMovie),
	}}
	require.NoError(t, w.Work(ctx, job))
	require.NoFileExists(t, filepath.Join(mediaDir, "movie.nfo"))
	require.Empty(t, generatedWrites.outputs)
}
