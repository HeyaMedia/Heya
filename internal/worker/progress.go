package worker

import (
	"sync"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/taskdefs"
)

// TaskProgressBroadcaster is the thin glue between work workers and
// the WebSocket. Workers call SetCurrent / Set when they pick up an item.
// When a River job carries scheduled_task_id, the broadcaster uses that
// explicit owner; otherwise it falls back to the unambiguous kind mapping.
//
// Counts (pending/running) are layered on by the periodic emitter in
// eventhub/periodic.go — this type only handles the "currently working
// on X" channel. Both halves use the same event type so the FE merges
// them into one per-task state.
type TaskProgressBroadcaster struct {
	hub        EventPublisher
	workToTask map[string]string

	// Last emitted payload per task, so pollers (the sonic runtime
	// heartbeat, dashboards) can snapshot "currently working on X"
	// without waiting for the next event. Events are fire-and-forget;
	// this is the only retained state.
	mu      sync.Mutex
	current map[string]eventhub.TaskProgressPayload
}

// NewTaskProgressBroadcaster returns a broadcaster wired to the event
// hub. nil-safe — Set / SetCurrentByKind become no-ops if hub is nil,
// which keeps tests + the `heya queue process` CLI working.
func NewTaskProgressBroadcaster(hub EventPublisher) *TaskProgressBroadcaster {
	return &TaskProgressBroadcaster{
		hub:        hub,
		workToTask: buildWorkToTaskMap(),
		current:    make(map[string]eventhub.TaskProgressPayload),
	}
}

// Current returns the most recent payload emitted for taskID, if any.
// Safe on a nil broadcaster.
func (b *TaskProgressBroadcaster) Current(taskID string) (eventhub.TaskProgressPayload, bool) {
	if b == nil {
		return eventhub.TaskProgressPayload{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.current[taskID]
	return p, ok
}

func (b *TaskProgressBroadcaster) emit(p eventhub.TaskProgressPayload) {
	// Progress is a presentation boundary. It can include paths copied from
	// legacy database rows, so redact generic URL credentials before emitting.
	p.CurrentItem = secrettext.Redact(p.CurrentItem)
	p.CurrentStage = secrettext.Redact(p.CurrentStage)
	b.mu.Lock()
	b.current[p.TaskID] = p
	b.mu.Unlock()
	b.hub.Emit(eventhub.EventTaskProgress, p)
}

// SetCurrentByKind emits a task.progress event with CurrentItem set,
// resolving the owning task ID from the kind. No-op when the kind is not
// tracked or is shared by multiple scheduled tasks without an explicit task ID.
func (b *TaskProgressBroadcaster) SetCurrentByKind(kind, item string) {
	b.SetCurrent(kind, "", item)
}

// SetCurrent emits a task.progress event with CurrentItem set. scheduledTaskID
// is preferred when present so shared child kinds (e.g. enrich_media_item) are
// attributed to the task that fanned them out.
func (b *TaskProgressBroadcaster) SetCurrent(kind, scheduledTaskID, item string) {
	if b == nil || b.hub == nil {
		return
	}
	taskID, ok := b.resolveTask(kind, scheduledTaskID)
	if !ok {
		return
	}
	b.emit(eventhub.TaskProgressPayload{
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
	b.emit(eventhub.TaskProgressPayload{
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
	b.SetStage(kind, "", item, stage)
}

// SetStage is the same as SetCurrent but also sets a finer-grained sub-step
// label.
func (b *TaskProgressBroadcaster) SetStage(kind, scheduledTaskID, item, stage string) {
	if b == nil || b.hub == nil {
		return
	}
	taskID, ok := b.resolveTask(kind, scheduledTaskID)
	if !ok {
		return
	}
	b.emit(eventhub.TaskProgressPayload{
		TaskID:       taskID,
		State:        "running",
		CurrentItem:  item,
		ItemKind:     kind,
		CurrentStage: stage,
	})
}

func (b *TaskProgressBroadcaster) resolveTask(kind, scheduledTaskID string) (string, bool) {
	if scheduledTaskID != "" && taskdefs.TaskOwnsKind(scheduledTaskID, kind) {
		return scheduledTaskID, true
	}
	taskID, ok := b.workToTask[kind]
	return taskID, ok
}

// buildWorkToTaskMap inverts the curated TaskKinds table so each work
// kind maps back to its owning scheduled task ID. Computed once at
// construction; the table is small and stable.
func buildWorkToTaskMap() map[string]string {
	return taskdefs.WorkToTask()
}
