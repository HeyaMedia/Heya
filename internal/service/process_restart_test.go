package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerRestartRequestIsAcknowledgedBeforeConsumption(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	app := &App{db: pool}

	result, err := app.RequestProcessRestart(ctx, "worker")
	require.NoError(t, err)
	require.NotEmpty(t, result.RequestID)

	handled, err := app.consumeWorkerRestart(ctx)
	require.NoError(t, err)
	assert.True(t, handled)

	raw, err := sqlc.New(pool).GetSystemSetting(ctx, workerRestartSettingKey)
	require.NoError(t, err)
	var request ProcessRestartRequest
	require.NoError(t, json.Unmarshal(raw, &request))
	assert.Equal(t, result.RequestID, request.ID)
	assert.NotNil(t, request.AcknowledgedAt)
	assert.NotEmpty(t, request.AcknowledgedBy)

	handled, err = app.consumeWorkerRestart(ctx)
	require.NoError(t, err)
	assert.False(t, handled, "an acknowledged request must not restart the replacement worker")
}

func TestProcessRestartRejectsUnknownTarget(t *testing.T) {
	_, err := (&App{}).RequestProcessRestart(context.Background(), "database")
	require.Error(t, err)
}
