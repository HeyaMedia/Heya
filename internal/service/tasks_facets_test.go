package service

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestFacetsTaskCoverageQueries(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupDB(t)
	app := &App{db: pool}
	q := sqlc.New(pool)

	_, err := app.QueryFacetsItems(ctx, "", 10, 0)
	require.NoError(t, err)
	_, err = app.QueryFacetsItems(ctx, "complete", 10, 0)
	require.NoError(t, err)
	_, err = app.QueryFacetsItems(ctx, "pending", 10, 0)
	require.NoError(t, err)

	_, err = q.CountPendingFullAnalysis(ctx, sqlc.CountPendingFullAnalysisParams{
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
	})
	require.NoError(t, err)
	_, err = q.CountPendingCLAPCleanup(ctx, sqlc.CountPendingCLAPCleanupParams{
		MaxDurationSeconds: sonicanalysis.MaxAnalysisDurationSeconds,
		AnalyzerVersion:    sonicanalysis.AnalyzerVersion,
		ClapWindows:        sonicanalysis.CurrentCLAPWindows,
	})
	require.NoError(t, err)
}
