package scheduler

import (
	"sync"
	"time"
)

type TaskProgress struct {
	TaskID      string `json:"task_id"`
	State       string `json:"state"`
	Total       int    `json:"total"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
	CurrentItem string `json:"current_item"`
	StartedAt   string `json:"started_at,omitempty"`
}

type ProgressTracker struct {
	mu          sync.RWMutex
	taskID      TaskID
	state       TaskState
	total       int
	completed   int
	failed      int
	currentItem string
	startedAt   time.Time
}

func NewProgressTracker(id TaskID, total int) *ProgressTracker {
	return &ProgressTracker{
		taskID:    id,
		state:     TaskRunning,
		total:     total,
		startedAt: time.Now(),
	}
}

func (p *ProgressTracker) Advance(item string) {
	p.mu.Lock()
	p.completed++
	p.currentItem = item
	p.mu.Unlock()
}

func (p *ProgressTracker) Fail(item string) {
	p.mu.Lock()
	p.completed++
	p.failed++
	p.currentItem = item
	p.mu.Unlock()
}

func (p *ProgressTracker) SetTotal(total int) {
	p.mu.Lock()
	p.total = total
	p.mu.Unlock()
}

func (p *ProgressTracker) SetCurrentItem(item string) {
	p.mu.Lock()
	p.currentItem = item
	p.mu.Unlock()
}

func (p *ProgressTracker) SetDiscovered(count int, item string) {
	p.mu.Lock()
	p.total = count
	p.completed = count
	p.currentItem = item
	p.mu.Unlock()
}

func (p *ProgressTracker) Snapshot() TaskProgress {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return TaskProgress{
		TaskID:      string(p.taskID),
		State:       string(p.state),
		Total:       p.total,
		Completed:   p.completed,
		Failed:      p.failed,
		CurrentItem: p.currentItem,
		StartedAt:   p.startedAt.UTC().Format(time.RFC3339),
	}
}

func (p *ProgressTracker) SetState(s TaskState) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
}
