package service

import (
	"encoding/hex"
	"testing"
)

// The derived id stands in for the persisted one whenever this process must
// not write to its database (passive mode borrowing production's). Clients
// key saved-server entries on it, so it has to look like the real thing and
// stay put across restarts.
func TestDerivedServerIDIsStableAndWellFormed(t *testing.T) {
	first := derivedServerID()
	if len(first) != 32 {
		t.Fatalf("derived id is %d chars, want 32 to match a minted id", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("derived id is not hex: %v", err)
	}
	if second := derivedServerID(); second != first {
		t.Errorf("derived id changed between calls (%s → %s); clients would see a new server on every restart", first, second)
	}
}
