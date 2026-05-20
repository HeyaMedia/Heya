package transcoder

import "sync"

type runEntry[T any] struct {
	done chan struct{}
	val  T
	err  error
}

type RunLock[T any] struct {
	mu      sync.Mutex
	running map[string]*runEntry[T]
}

func NewRunLock[T any]() *RunLock[T] {
	return &RunLock[T]{
		running: make(map[string]*runEntry[T]),
	}
}

func (r *RunLock[T]) Do(key string, fn func() (T, error)) (T, error) {
	r.mu.Lock()
	if entry, ok := r.running[key]; ok {
		r.mu.Unlock()
		<-entry.done
		return entry.val, entry.err
	}

	entry := &runEntry[T]{done: make(chan struct{})}
	r.running[key] = entry
	r.mu.Unlock()

	entry.val, entry.err = fn()
	close(entry.done)

	r.mu.Lock()
	delete(r.running, key)
	r.mu.Unlock()

	return entry.val, entry.err
}
