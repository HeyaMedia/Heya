package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWorkerRuntimeStatusOnline(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		status WorkerRuntimeStatus
		want   bool
	}{
		{name: "fresh", status: WorkerRuntimeStatus{Running: true, HeartbeatAt: now.Add(-10 * time.Second)}, want: true},
		{name: "boundary", status: WorkerRuntimeStatus{Running: true, HeartbeatAt: now.Add(-workerRuntimeStaleAfter)}, want: true},
		{name: "stale", status: WorkerRuntimeStatus{Running: true, HeartbeatAt: now.Add(-workerRuntimeStaleAfter - time.Nanosecond)}},
		{name: "gracefully stopped", status: WorkerRuntimeStatus{Running: false, HeartbeatAt: now}},
		{name: "missing", status: WorkerRuntimeStatus{}},
		{name: "future timestamp", status: WorkerRuntimeStatus{Running: true, HeartbeatAt: now.Add(time.Second)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.Online(now))
		})
	}
}
