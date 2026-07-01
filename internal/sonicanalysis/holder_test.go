package sonicanalysis

import (
	"errors"
	"testing"
)

// Reconfigure with no leases outstanding applies immediately.
func TestHolderReconfigureIdle(t *testing.T) {
	h := NewHolder(Config{Accelerator: AccelCPU}, 0)
	if err := h.Reconfigure(Config{Accelerator: AccelCoreML}); err != nil {
		t.Fatalf("idle reconfigure: %v", err)
	}
	if h.cfg.Accelerator != AccelCoreML {
		t.Fatalf("cfg not applied: %s", h.cfg.Accelerator)
	}
	if h.pendingCfg != nil {
		t.Fatal("pendingCfg should be nil after immediate apply")
	}
}

// Reconfigure while leased returns ErrHolderBusy but stashes the config,
// which is applied when the last lease is released — the "retry at idle".
func TestHolderReconfigureBusyAppliesOnRelease(t *testing.T) {
	h := NewHolder(Config{Accelerator: AccelCPU}, 0)
	h.refs = 1 // simulate an outstanding lease without loading models

	err := h.Reconfigure(Config{Accelerator: AccelCoreML})
	if !errors.Is(err, ErrHolderBusy) {
		t.Fatalf("expected ErrHolderBusy, got %v", err)
	}
	if h.cfg.Accelerator != AccelCPU {
		t.Fatalf("cfg must not change while leased: %s", h.cfg.Accelerator)
	}
	if h.pendingCfg == nil || h.pendingCfg.Accelerator != AccelCoreML {
		t.Fatal("pending config not stashed")
	}
	if st := h.Status(); st.PendingAccelerator == nil || *st.PendingAccelerator != AccelCoreML {
		t.Fatal("Status should surface the pending accelerator")
	}

	h.release() // last lease drains → pending applies

	if h.cfg.Accelerator != AccelCoreML {
		t.Fatalf("pending cfg not applied at idle: %s", h.cfg.Accelerator)
	}
	if h.pendingCfg != nil {
		t.Fatal("pendingCfg should be cleared after apply")
	}
	if h.refs != 0 {
		t.Fatalf("refs = %d, want 0", h.refs)
	}
	if st := h.Status(); st.PendingAccelerator != nil {
		t.Fatal("Status should clear pending after apply")
	}
}

// A newer Reconfigure while still busy replaces the stashed config —
// last write wins.
func TestHolderReconfigureBusyLastWriteWins(t *testing.T) {
	h := NewHolder(Config{Accelerator: AccelCPU}, 0)
	h.refs = 1

	_ = h.Reconfigure(Config{Accelerator: AccelCoreML})
	_ = h.Reconfigure(Config{Accelerator: AccelCUDA})
	h.release()

	if h.cfg.Accelerator != AccelCUDA {
		t.Fatalf("want last pending write (cuda), got %s", h.cfg.Accelerator)
	}
}

// While leases remain outstanding after a release, the pending config
// stays stashed until the true refs→0 transition.
func TestHolderPendingWaitsForLastLease(t *testing.T) {
	h := NewHolder(Config{Accelerator: AccelCPU}, 0)
	h.refs = 2

	_ = h.Reconfigure(Config{Accelerator: AccelCoreML})
	h.release()

	if h.cfg.Accelerator != AccelCPU || h.pendingCfg == nil {
		t.Fatal("pending must not apply while a lease is still outstanding")
	}

	h.release()

	if h.cfg.Accelerator != AccelCoreML || h.pendingCfg != nil {
		t.Fatal("pending must apply when the last lease drains")
	}
}
