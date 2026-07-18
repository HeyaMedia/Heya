package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSonicAnalysisSettingsPreservesEnvLockedDBFields(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	app := &App{db: pool}

	t.Setenv(sonicEnvEnabled, "true")
	require.NoError(t, app.SetSystemSetting(ctx, sonicSettingsKey, []byte(`{"enabled":false,"accelerator":"cpu"}`)))

	require.NoError(t, app.SetSonicAnalysisSettings(ctx, SonicAnalysisSettings{
		Enabled:     true,
		Accelerator: "directml",
	}))

	persisted := app.sonicAnalysisSettingsFromDB(ctx)
	assert.False(t, persisted.Enabled, "env-locked enabled should keep its DB value")
	assert.Equal(t, "directml", persisted.Accelerator, "unlocked accelerator should persist")
}

func TestStrictSonicSettingsReadDoesNotTurnDatabaseFailureIntoDefaults(t *testing.T) {
	wantErr := errors.New("database unavailable")
	_, err := effectiveSonicAnalysisSettingsStrict(context.Background(), func(context.Context, string) (json.RawMessage, error) {
		return nil, wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

func TestWorkerSonicHolderReconcilesPersistedAccelerator(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	t.Setenv(sonicEnvAccelerator, "")
	require.NoError(t, sqlc.New(pool).UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{
		Key:   sonicSettingsKey,
		Value: []byte(`{"enabled":true,"accelerator":"cpu"}`),
	}))

	holder := sonicanalysis.NewHolder(sonicanalysis.Config{
		ModelsDir:   t.TempDir(),
		Accelerator: sonicanalysis.AccelAuto,
	}, 0)
	app := &App{
		db:          pool,
		config:      &config.Config{DataDir: config.Field[string]{Value: t.TempDir()}},
		sonicHolder: holder,
	}

	require.NoError(t, app.reconcileSonicHolderSettings(ctx))
	assert.Equal(t, sonicanalysis.AccelCPU, holder.Status().Accelerator)
}
