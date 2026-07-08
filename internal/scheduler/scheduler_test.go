package scheduler

import (
	"testing"
	"time"
)

func TestInitialNextRunAfter(t *testing.T) {
	now := time.Date(2026, 7, 8, 21, 30, 0, 0, time.UTC)
	next := InitialNextRunAfter(now, 24, "02:00", "06:00")
	want := time.Date(2026, 7, 9, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("outside window next run: got %s, want %s", next, want)
	}

	now = time.Date(2026, 7, 8, 1, 30, 0, 0, time.UTC)
	next = InitialNextRunAfter(now, 24, "23:00", "02:00")
	want = time.Date(2026, 7, 9, 1, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("inside overnight window next run: got %s, want %s", next, want)
	}
}
