package eventhub

import "time"

type EventType string

const (
	EventLog            EventType = "log"
	EventScanStarted    EventType = "scan.started"
	EventScanCompleted  EventType = "scan.completed"
	EventMediaAdded     EventType = "media.added"
	EventMediaUpdated   EventType = "media.updated"
	EventMediaRemoved   EventType = "media.removed"
	EventMediaWatched   EventType = "media.watched"
	EventLibraryDeleted EventType = "library.deleted"
	EventQueueStatus    EventType = "queue.status"
	EventActiveJobs     EventType = "active_jobs"
	EventStatsUpdated   EventType = "stats.updated"
	EventScanProgress   EventType = "scan.progress"
	EventTaskProgress   EventType = "task.progress"
	EventTailscale      EventType = "tailscale.status"
	// Radio ICY metadata — fired by the radio stream proxy each time an
	// upstream station sends a fresh `StreamTitle=...` block. FE consumers
	// (Playbar / QueueRow) overlay these on the "Now Playing" card while a
	// live stream is the active track.
	EventRadioICY EventType = "radio.icy"
)

// RadioICYPayload is the per-user event body for EventRadioICY. UserID
// scoping is done by the hub so the FE only sees its own station's
// metadata; the stream URL is echoed so the FE can match the event to
// the currently-playing station when multiple tabs share a session.
type RadioICYPayload struct {
	Artist    string `json:"artist"`
	Title     string `json:"title"`
	StreamURL string `json:"stream_url"`
}

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
	// Phase + Total + Done are used by the music post-scan fan-out to report
	// per-artist refresh progress on EventScanProgress (e.g. "refresh 17/200").
	Phase      string `json:"phase,omitempty"` // "scan" | "match" | "refresh"
	Total      int    `json:"total,omitempty"`
	Done       int    `json:"done,omitempty"`
	CurrentRef string `json:"current_ref,omitempty"` // e.g. artist name
}

type MediaPayload struct {
	MediaItemID int64  `json:"media_item_id"`
	LibraryID   int64  `json:"library_id,omitempty"`
	Title       string `json:"title,omitempty"`
	MediaType   string `json:"media_type,omitempty"`
}

// LibraryPayload is the body for library lifecycle events (currently just
// library.deleted). Deleting a library cascades server-side across an entire
// media type, so the FE uses this to blow away its cached catalog data;
// MediaType is carried for consumers that want to scope the invalidation.
type LibraryPayload struct {
	LibraryID int64  `json:"library_id"`
	Name      string `json:"name,omitempty"`
	MediaType string `json:"media_type,omitempty"`
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
	LibraryID int64  `json:"library_id"`
	Name      string `json:"name"`
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Matched   int    `json:"matched"`
	Unmatched int    `json:"unmatched"`
	Errors    int    `json:"errors"`
}

// TaskProgressPayload is the WebSocket carrier for "what's happening
// right now" across the six scheduled tasks. Replaces the in-process
// ProgressTracker snapshot that used to ride this event before the
// kickoff/work-job split.
//
// Two sources merge into the same event:
//
//  1. Per-job emissions from work workers (analyze_track_facets,
//     trickplay_file, etc.) carry CurrentItem + ItemKind. State is
//     "running". Counts are zero (the UI keeps the last counts it
//     saw).
//  2. The activity ticker in periodic.go emits one event per task
//     every 2 seconds with Pending + Running counts, no CurrentItem.
//     State is "running" when either count > 0; "idle" otherwise.
//
// The frontend merges into a per-task state dict keyed by TaskID:
// counts come from (2), current_item / item_kind come from (1).
type TaskProgressPayload struct {
	TaskID      string `json:"task_id"`
	State       string `json:"state"`
	Pending     int    `json:"pending,omitempty"`
	Running     int    `json:"running,omitempty"`
	CurrentItem string `json:"current_item,omitempty"`
	ItemKind    string `json:"item_kind,omitempty"`
	// CurrentStage is a finer-grained "within the current item" label
	// — currently only populated by analyze_track_facets, which fires
	// one event per pipeline stage (CLAP audio embed, Discogs heads,
	// etc.) on top of the per-item event. Empty for everything else.
	CurrentStage string `json:"current_stage,omitempty"`
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
