package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginGuardUsesIndependentIPAndAccountBuckets(t *testing.T) {
	g := NewLoginGuard()
	for i := 0; i < 5; i++ {
		assert.True(t, g.Allow("203.0.113."+string(rune('a'+i)), "alice"))
	}
	assert.False(t, g.Allow("203.0.113.99", "alice"), "rotating IPs must still exhaust the account bucket")
	assert.True(t, g.Allow("203.0.113.99", "bob"), "a different account retains its own allowance")
}

func TestLoginGuardSuccessfulAccountClearDoesNotClearIP(t *testing.T) {
	g := NewLoginGuard()
	for i := 0; i < 10; i++ {
		assert.True(t, g.Allow("203.0.113.7", "user"+string(rune('a'+i))))
	}
	g.ClearAccount("usera")
	assert.False(t, g.Allow("203.0.113.7", "usera"))
}

func TestLoginGuardPasswordSlotsAreBounded(t *testing.T) {
	g := NewLoginGuard()
	releases := make([]func(), 0, cap(g.passwordSlots))
	for range cap(g.passwordSlots) {
		release, ok := g.BeginPasswordCheck()
		require.True(t, ok)
		releases = append(releases, release)
	}
	_, ok := g.BeginPasswordCheck()
	assert.False(t, ok)
	for _, release := range releases {
		release()
	}
	stats := g.Stats()
	assert.Equal(t, uint64(cap(g.passwordSlots)), stats.PasswordChecksStarted)
	assert.Equal(t, uint64(1), stats.SaturatedTotal)
	assert.Zero(t, stats.PasswordChecksActive)
}

func TestLoginGuardReportsAggregateBucketStats(t *testing.T) {
	g := NewLoginGuard()
	assert.True(t, g.Allow("203.0.113.7", "alice"))
	stats := g.Stats()
	assert.Equal(t, uint64(1), stats.AllowedTotal)
	assert.Equal(t, 1, stats.ActiveIPBuckets)
	assert.Equal(t, 1, stats.ActiveAccountBuckets)
	assert.GreaterOrEqual(t, stats.PasswordCheckCapacity, 2)
}
