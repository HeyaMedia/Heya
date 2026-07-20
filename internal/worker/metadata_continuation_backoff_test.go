package worker

import (
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

func TestMetadataSearchRetryBackoffScalesWithWaitingPopulation(t *testing.T) {
	backoff := newMetadataContinuationBackoff()

	tests := []struct {
		waiting int64
		want    time.Duration
	}{
		{waiting: 0, want: time.Minute},
		{waiting: 99, want: time.Minute},
		{waiting: 100, want: 70 * time.Second},
		{waiting: 499, want: 100 * time.Second},
		{waiting: 2_400, want: 5 * time.Minute},
		{waiting: 50_000, want: 5 * time.Minute},
	}

	for _, tt := range tests {
		backoff.mu.Lock()
		backoff.searchWaiting[sqlc.MediaTypeMusic] = tt.waiting
		backoff.mu.Unlock()
		got, waiting := backoff.searchRetryAfter(sqlc.MediaTypeMusic, 30*time.Second)
		require.Equal(t, tt.waiting, waiting)
		require.Equal(t, tt.want, got)
	}
}

func TestMetadataSearchRetryBackoffHonorsLongerProviderDelay(t *testing.T) {
	backoff := newMetadataContinuationBackoff()
	backoff.mu.Lock()
	backoff.searchWaiting[sqlc.MediaTypeMusic] = 100
	backoff.mu.Unlock()

	got, _ := backoff.searchRetryAfter(sqlc.MediaTypeMusic, 12*time.Minute)
	require.Equal(t, 12*time.Minute, got)
}

func TestMetadataSearchRetryBackoffIsIndependentByMediaType(t *testing.T) {
	backoff := newMetadataContinuationBackoff()
	backoff.mu.Lock()
	backoff.searchWaiting[sqlc.MediaTypeMusic] = 50_000
	backoff.searchWaiting[sqlc.MediaTypeTv] = 0
	backoff.mu.Unlock()

	musicDelay, musicWaiting := backoff.searchRetryAfter(sqlc.MediaTypeMusic, 30*time.Second)
	tvDelay, tvWaiting := backoff.searchRetryAfter(sqlc.MediaTypeTv, 30*time.Second)
	require.EqualValues(t, 50_000, musicWaiting)
	require.Equal(t, 5*time.Minute, musicDelay)
	require.Zero(t, tvWaiting)
	require.Equal(t, time.Minute, tvDelay)
}

func TestMetadataSearchEventReconciliationIsSlowAndStable(t *testing.T) {
	workflowID := "122ca081-208f-4031-be0e-20328769c8c4"
	first := metadataSearchReconcileAfter(workflowID, 30*time.Second)
	second := metadataSearchReconcileAfter(workflowID, 30*time.Second)

	require.Equal(t, first, second)
	require.GreaterOrEqual(t, first, metadataSearchReconcileMinimum)
	require.Less(t, first, metadataSearchReconcileMinimum+metadataSearchReconcileSpread)
}

func TestMetadataSearchEventReconciliationHonorsLongerProviderDelay(t *testing.T) {
	got := metadataSearchReconcileAfter("122ca081-208f-4031-be0e-20328769c8c4", 2*time.Hour)
	require.Equal(t, 2*time.Hour, got)
}
