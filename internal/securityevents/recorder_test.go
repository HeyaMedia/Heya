package securityevents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecorderBoundsHistoryAndCountsLifetimeSignals(t *testing.T) {
	recorder := New(2)
	recorder.Record(SecurityEvent{Kind: KindLoginFailed, AccountKey: "first"})
	recorder.Record(SecurityEvent{Kind: KindWAFMatch, RuleID: "942100"})
	recorder.Record(SecurityEvent{Kind: KindWAFBlock, Path: "/api/search"})

	snapshot := recorder.Snapshot(10)
	assert.Equal(t, uint64(1), snapshot.Counters.LoginFailures)
	assert.Equal(t, uint64(1), snapshot.Counters.WAFMatches)
	assert.Equal(t, uint64(1), snapshot.Counters.WAFBlocked)
	assert.Len(t, snapshot.Recent, 2)
	assert.Equal(t, KindWAFMatch, snapshot.Recent[0].Kind)
	assert.Equal(t, KindWAFBlock, snapshot.Recent[1].Kind)
}

func TestRecorderBoundsUntrustedFields(t *testing.T) {
	recorder := New(1)
	recorder.Record(SecurityEvent{Kind: KindWAFMatch, Message: string(make([]byte, 400)), Path: string(make([]byte, 700))})
	event := recorder.Snapshot(1).Recent[0]
	assert.Len(t, event.Message, 280)
	assert.Len(t, event.Path, 512)
}
