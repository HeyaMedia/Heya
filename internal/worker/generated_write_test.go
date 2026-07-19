package worker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

type recordingGeneratedWriteSuppressor struct {
	outputs  []generatedwrite.Output
	err      error
	failures int
}

func (r *recordingGeneratedWriteSuppressor) SuppressGeneratedWrite(output generatedwrite.Output) error {
	r.outputs = append(r.outputs, output)
	if r.failures > 0 {
		r.failures--
		return r.err
	}
	return nil
}

func TestPublishGeneratedWriteDiscardsStagingWhenSettingChanges(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	root := t.TempDir()
	userID := testutil.TestUserID(t, pool)
	library, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name: "generated-write-post-stage-setting-test", MediaType: sqlc.MediaTypeMovie, Paths: []string{root},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    userID, Settings: []byte(`{"save_nfo":true}`),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, library.ID) })

	target := filepath.Join(root, "movie.nfo")
	prepared, err := generatedwrite.PrepareBytes(target, 0o644, []byte("generated"))
	require.NoError(t, err)
	_, err = q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       library.ID,
		Settings: []byte(`{"save_nfo":false}`),
	})
	require.NoError(t, err)

	generatedWrites := &recordingGeneratedWriteSuppressor{}
	require.NoError(t, publishGeneratedWriteWhenAllowed(
		ctx, pool, generatedWrites, q, library.ID, generatedWriteNFO, prepared, nil,
	))
	require.NoFileExists(t, target)
	require.Empty(t, generatedWrites.outputs)

	_, err = q.UpdateLibrarySettings(ctx, sqlc.UpdateLibrarySettingsParams{
		ID:       library.ID,
		Settings: []byte(`{"save_nfo":true}`),
	})
	require.NoError(t, err)
	staleTarget := filepath.Join(root, "stale-movie.nfo")
	stalePrepared, err := generatedwrite.PrepareBytes(staleTarget, 0o644, []byte("stale"))
	require.NoError(t, err)
	require.NoError(t, publishGeneratedWriteWhenAllowed(
		ctx, pool, generatedWrites, q, library.ID, generatedWriteNFO, stalePrepared,
		func(context.Context) (bool, error) { return false, nil },
	))
	require.NoFileExists(t, staleTarget)
	require.Empty(t, generatedWrites.outputs, "failed post-stage ownership check must not publish provenance")
}
