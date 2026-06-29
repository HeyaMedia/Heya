package service

import (
	"context"
	"testing"

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
