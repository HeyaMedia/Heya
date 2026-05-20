package eventhub

import "time"

type EventType string

const (
	EventLog          EventType = "log"
	EventScanStarted  EventType = "scan.started"
	EventScanCompleted EventType = "scan.completed"
	EventMediaAdded   EventType = "media.added"
	EventMediaUpdated EventType = "media.updated"
	EventMediaRemoved EventType = "media.removed"
	EventMediaWatched EventType = "media.watched"
	EventQueueStatus   EventType = "queue.status"
	EventActiveJobs    EventType = "active_jobs"
	EventStatsUpdated  EventType = "stats.updated"
	EventScanProgress  EventType = "scan.progress"
)

type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"ts"`
	Payload   any       `json:"payload"`
}

type LogPayload struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type ScanPayload struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name,omitempty"`
	Discovered  int    `json:"discovered,omitempty"`
	New         int    `json:"new,omitempty"`
	Missing     int    `json:"missing,omitempty"`
}

type MediaPayload struct {
	MediaItemID int64  `json:"media_item_id"`
	LibraryID   int64  `json:"library_id,omitempty"`
	Title       string `json:"title,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
}

type WatchPayload struct {
	UserID      int64 `json:"user_id"`
	MediaItemID int64 `json:"media_item_id"`
	Progress    int32 `json:"progress_seconds"`
	Total       int32 `json:"total_seconds"`
	Completed   bool  `json:"completed"`
}

type QueueStatusPayload struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
}

type ActiveJob struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	Queue     string    `json:"queue"`
	StartedAt time.Time `json:"started_at,omitempty"`
	ArgsJSON  string    `json:"args,omitempty"`
}

type ActiveJobsPayload struct {
	Jobs []ActiveJob `json:"jobs"`
}

type ScanProgressPayload struct {
	Libraries []LibraryScanProgress `json:"libraries"`
}

type LibraryScanProgress struct {
	LibraryID  int64  `json:"library_id"`
	Name       string `json:"name"`
	Total      int    `json:"total"`
	Processed  int    `json:"processed"`
	Matched    int    `json:"matched"`
	Unmatched  int    `json:"unmatched"`
	Errors     int    `json:"errors"`
}

type StatsPayload struct {
	Libraries    int            `json:"libraries"`
	MediaCounts  map[string]int `json:"media_counts"`
	TotalMedia   int            `json:"total_media"`
	TotalPeople  int            `json:"total_people"`
	TotalFiles   int            `json:"total_files"`
	QueuePending int            `json:"queue_pending"`
	QueueRunning int            `json:"queue_running"`
}
