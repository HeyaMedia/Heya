package service

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/securityevents"
	"github.com/stretchr/testify/assert"
)

func TestSecurityStatusReportsEffectiveConfigAndAggregateEvents(t *testing.T) {
	app := &App{
		config: &config.Config{
			EnableRegistration: config.Field[bool]{Value: false, Source: config.SourceEnv, EnvVar: "HEYA_ENABLE_REGISTRATION"},
			WAFMode:            config.Field[string]{Value: "block", Source: config.SourceEnv, EnvVar: "HEYA_WAF_MODE"},
			TrustedNetworks:    config.Field[string]{Value: "100.64.0.0/10,192.168.0.0/16", Source: config.SourceDefault},
		},
		securityEvents: securityevents.New(8),
	}
	app.SecurityEvents().Record(securityevents.SecurityEvent{Kind: securityevents.KindWAFMatch, RuleID: "942100"})

	status := app.SecurityStatus(context.Background())
	assert.Equal(t, "disabled", status.Registration.State)
	assert.Equal(t, config.SourceEnv, status.Registration.Source)
	assert.Equal(t, "block", status.WAF.Value)
	assert.True(t, status.WAF.Enabled)
	assert.True(t, status.WAF.Blocking)
	assert.Equal(t, []string{"100.64.0.0/10", "192.168.0.0/16"}, status.TrustedNetworks.Networks)
	assert.True(t, status.TrustedNetworks.RuntimeEditable)
	assert.Equal(t, uint64(1), status.Events.Counters.WAFMatches)
	assert.Equal(t, auth.MinPasswordLength, status.Password.MinimumLength)
}

func TestTrustedClientIPUsesDirectPeerCIDRs(t *testing.T) {
	app := &App{config: &config.Config{TrustedNetworks: config.Field[string]{
		Value: "100.64.0.0/10,192.168.0.0/16", Source: config.SourceDefault,
	}}}
	assert.True(t, app.TrustedClientIP("100.76.110.94"))
	assert.True(t, app.TrustedClientIP("192.168.10.10"))
	assert.False(t, app.TrustedClientIP("203.0.113.7"))
	assert.False(t, app.TrustedClientIP("unknown"))
}
