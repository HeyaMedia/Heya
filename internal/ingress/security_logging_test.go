package ingress

import (
	"testing"

	"github.com/karbowiak/heya/internal/securityevents"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestSecurityLogCoreCapturesSanitizedCorazaMatch(t *testing.T) {
	recorder := securityevents.New(8)
	manager := New(nil, zerolog.Nop(), recorder)
	previous := activeManager.Swap(manager)
	t.Cleanup(func() { activeManager.Store(previous) })

	core := &heyaSecurityLogCore{}
	err := core.Write(zapcore.Entry{
		LoggerName: "http.handlers.waf",
		Message:    `[client "203.0.113.7"] Coraza: Warning. match [id "942100"] [msg "SQL Injection Attack Detected"] [data "password=do-not-retain"] [severity "CRITICAL"] [uri "/api/search?q=secret"] [unique_id "tx-123"]`,
	}, nil)
	require.NoError(t, err)

	events := recorder.Snapshot(8).Recent
	require.Len(t, events, 1)
	assert.Equal(t, securityevents.KindWAFMatch, events[0].Kind)
	assert.Equal(t, "942100", events[0].RuleID)
	assert.Equal(t, "critical", events[0].Severity)
	assert.Equal(t, "/api/search", events[0].Path)
	assert.NotContains(t, events[0].Message, "do-not-retain")
}

func TestSecurityLogCoreCapturesBlockedRequestFieldsWithoutQuery(t *testing.T) {
	recorder := securityevents.New(8)
	manager := New(nil, zerolog.Nop(), recorder)
	previous := activeManager.Swap(manager)
	t.Cleanup(func() { activeManager.Store(previous) })

	core := &heyaSecurityLogCore{}
	err := core.Write(zapcore.Entry{LoggerName: "http.handlers.waf", Message: "WAF rule violation detected"}, []zapcore.Field{
		{Key: "client_ip", Type: zapcore.StringType, String: "203.0.113.8:54321"},
		{Key: "uri", Type: zapcore.StringType, String: "/api/search?token=secret"},
		{Key: "unique_id", Type: zapcore.StringType, String: "tx-456"},
	})
	require.NoError(t, err)

	events := recorder.Snapshot(8).Recent
	require.Len(t, events, 1)
	assert.Equal(t, securityevents.KindWAFBlock, events[0].Kind)
	assert.Equal(t, "203.0.113.8", events[0].ClientIP)
	assert.Equal(t, "/api/search", events[0].Path)
}
