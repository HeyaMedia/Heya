package cast

import (
	"context"
	"testing"
)

// The Settings toggle disables and re-enables casting live. A re-enable
// must fully rebuild the manager: a Stop that leaves `started` set (or a
// canceled runCtx behind) makes every later transport spawn inherit a
// dead context and fail — the exact regression this test pins.
func TestManagerStopStartCycle(t *testing.T) {
	m := New(t.TempDir())

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("first start: %v", err)
	}
	ctx1, err := m.transportCtx()
	if err != nil {
		t.Fatalf("transportCtx after start: %v", err)
	}
	if ctx1.Err() != nil {
		t.Fatal("fresh runCtx is already canceled")
	}

	m.Stop()
	if _, err := m.transportCtx(); err == nil {
		t.Fatal("transportCtx should error while stopped")
	}
	if _, err := m.Play("airplay:nope", 1, TrackInfo{}, 30); err == nil {
		t.Fatal("Play should error while stopped")
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	ctx2, err := m.transportCtx()
	if err != nil {
		t.Fatalf("transportCtx after restart: %v", err)
	}
	if ctx2.Err() != nil {
		t.Fatal("runCtx after restart is canceled — Stop did not reset the lifecycle")
	}

	m.Stop()
}
