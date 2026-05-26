package worker

import (
	"github.com/karbowiak/heya/internal/eventhub"
)

// TaskProgressBroadcaster is the thin glue between work workers and
// the WebSocket. Workers call SetCurrentByKind / Set when they pick up
// an item, the broadcaster resolves which scheduled task that kind
// belongs to (via the inverted TaskKinds table) and fires a
// task.progress event with the human-readable label.
//
// Counts (pending/running) are layered on by the periodic emitter in
// eventhub/periodic.go — this type only handles the "currently working
// on X" channel. Both halves use the same event type so the FE merges
// them into one per-task state.
type TaskProgressBroadcaster struct {
	hub        EventPublisher
	workToTask map[string]string
}

// NewTaskProgressBroadcaster returns a broadcaster wired to the event
// hub. nil-safe — Set / SetCurrentByKind become no-ops if hub is nil,
// which keeps tests + the `heya queue process` CLI working.
func NewTaskProgressBroadcaster(hub EventPublisher) *TaskProgressBroadcaster {
	return &TaskProgressBroadcaster{
		hub:        hub,
		workToTask: buildWorkToTaskMap(),
	}
}

// SetCurrentByKind emits a task.progress event with CurrentItem set,
// resolving the owning task ID from the kind. No-op when the kind
// isn't tracked (e.g. download_image, save_nfo — these aren't part of
// any scheduled task's flow).
func (b *TaskProgressBroadcaster) SetCurrentByKind(kind, item string) {
	if b == nil || b.hub == nil {
		return
	}
	taskID, ok := b.workToTask[kind]
	if !ok {
		return
	}
	b.hub.Emit(eventhub.EventTaskProgress, eventhub.TaskProgressPayload{
		TaskID:      taskID,
		State:       "running",
		CurrentItem: item,
		ItemKind:    kind,
	})
}

// Set emits a task.progress event for a specific task ID directly.
// Used when a worker knows its task but not a single owning kind
// (rare — most callers should use SetCurrentByKind).
func (b *TaskProgressBroadcaster) Set(taskID, kind, item string) {
	if b == nil || b.hub == nil {
		return
	}
	b.hub.Emit(eventhub.EventTaskProgress, eventhub.TaskProgressPayload{
		TaskID:      taskID,
		State:       "running",
		CurrentItem: item,
		ItemKind:    kind,
	})
}

// SetStageByKind is the same as SetCurrentByKind but also sets a
// finer-grained sub-step label. Used by analyze_track_facets to
// surface the pipeline stage (Discogs heads, CLAP audio, etc.) on top
// of the per-track event. The FE shows item + stage on separate lines.
func (b *TaskProgressBroadcaster) SetStageByKind(kind, item, stage string) {
	if b == nil || b.hub == nil {
		return
	}
	taskID, ok := b.workToTask[kind]
	if !ok {
		return
	}
	b.hub.Emit(eventhub.EventTaskProgress, eventhub.TaskProgressPayload{
		TaskID:       taskID,
		State:        "running",
		CurrentItem:  item,
		ItemKind:     kind,
		CurrentStage: stage,
	})
}

// buildWorkToTaskMap inverts the curated TaskKinds table so each work
// kind maps back to its owning scheduled task ID. Computed once at
// construction; the table is small and stable.
func buildWorkToTaskMap() map[string]string {
	out := map[string]string{}
	for _, taskID := range scheduledTaskIDs {
		for _, kind := range TaskKinds(taskID) {
			// First task to claim a kind wins. TaskKinds() is curated
			// so each kind is exclusive to one task — no collisions
			// today.
			if _, exists := out[kind]; !exists {
				out[kind] = taskID
			}
		}
	}
	return out
}

// scheduledTaskIDs lists every task this binary knows about — both
// the six scheduled-task IDs (rows in scheduled_tasks, kept in
// lock-step with kickoffTaskIDs in kickoff_jobs.go and the enum in
// jobs_huma.go) AND the synthetic buckets that group ad-hoc workers
// for UI display. The name "scheduledTaskIDs" is a historical
// artifact; it's actually the iteration source for the work→task
// reverse-lookup map, which needs both kinds.
var scheduledTaskIDs = []string{
	// Scheduled tasks.
	"scan_libraries",
	"refresh_stale_items",
	"scan_music_loudness",
	"generate_trickplay",
	"generate_thumbnails",
	"analyze_music_facets",
	// Synthetic buckets — no DB row, but every ad-hoc worker kind
	// lives in one so its progress shows up as a labelled card.
	"transcoding",
	"artwork",
	"nfo_writes",
	"external_lookups",
	"refresh_actions",
	"cleanup",
}
