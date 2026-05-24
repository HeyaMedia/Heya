package scheduler

import "context"

type TaskID string

const (
	TaskGenerateTrickplay  TaskID = "generate_trickplay"
	TaskGenerateThumbnails TaskID = "generate_thumbnails"
	TaskScanLibraries      TaskID = "scan_libraries"
	TaskRefreshStaleItems  TaskID = "refresh_stale_items"
	TaskScanMusicLoudness  TaskID = "scan_music_loudness"
	TaskAnalyzeMusicFacets TaskID = "analyze_music_facets"
)

type TaskState string

const (
	TaskIdle    TaskState = "idle"
	TaskRunning TaskState = "running"
)

type Task interface {
	ID() TaskID
	CountPending(ctx context.Context) (int, error)
	Run(ctx context.Context, progress *ProgressTracker) error
}
