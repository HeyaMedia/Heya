package scheduler

import "context"

type TaskID string

const (
	TaskGenerateTrickplay   TaskID = "generate_trickplay"
	TaskGenerateThumbnails  TaskID = "generate_thumbnails"
	TaskScanLibraries       TaskID = "scan_libraries"
	TaskRefreshMetadata     TaskID = "refresh_metadata"
	TaskRefreshMusicArtists TaskID = "refresh_music_artists"
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
