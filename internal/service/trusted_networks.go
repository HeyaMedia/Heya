package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/trustednetworks"
	"github.com/rs/zerolog/log"
)

type TrustedNetworksStatus struct {
	Networks        []string      `json:"networks"`
	Source          config.Source `json:"source"`
	EnvVar          string        `json:"env_var,omitempty"`
	RuntimeEditable bool          `json:"runtime_editable"`
	WAFBypass       bool          `json:"waf_bypass"`
	RateLimitBypass bool          `json:"rate_limit_bypass"`
}

// LoadTrustedNetworksFromDB applies the persisted allowlist when the env does
// not own it. Malformed stored values fail closed by leaving the safe built-in
// defaults active; a later valid admin save replaces the bad row.
func (a *App) LoadTrustedNetworksFromDB(ctx context.Context) {
	if a == nil {
		return
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.config == nil || a.config.TrustedNetworks.Source != config.SourceDefault {
		return
	}
	values, ok := readSetting[[]string](a, ctx, trustednetworks.SettingKey)
	if !ok {
		return
	}
	canonical, _, err := trustednetworks.CanonicalList(values)
	if err != nil {
		log.Warn().Err(err).Msg("ignoring invalid persisted trusted-network policy")
		return
	}
	a.config.TrustedNetworks = config.Field[string]{Value: canonical, Source: config.SourceDB}
}

func (a *App) TrustedNetworksStatus() TrustedNetworksStatus {
	field := config.Field[string]{Value: trustednetworks.DefaultValue, Source: config.SourceDefault}
	if snapshot := a.ConfigSnapshot(); snapshot != nil {
		field = snapshot.TrustedNetworks
	}
	_, values, err := trustednetworks.Canonical(field.Value)
	if err != nil {
		values = []string{}
	}
	return TrustedNetworksStatus{
		Networks: values, Source: normalizedSource(field.Source), EnvVar: field.EnvVar,
		RuntimeEditable: field.Source != config.SourceEnv,
		WAFBypass:       true, RateLimitBypass: true,
	}
}

// TrustedClientIP matches only the direct peer established by requestmeta.
// Forwarding headers are intentionally outside this trust decision.
func (a *App) TrustedClientIP(value string) bool {
	snapshot := a.ConfigSnapshot()
	if snapshot == nil {
		return false
	}
	prefixes, err := trustednetworks.Parse(snapshot.TrustedNetworks.Value)
	return err == nil && trustednetworks.Contains(prefixes, value)
}

// SaveAndApplyTrustedNetworks validates, live-applies, persists, and publishes
// one allowlist update. Caddy's reload is atomic; if persistence fails after a
// successful reload, the previous ingress policy is restored before returning.
func (a *App) SaveAndApplyTrustedNetworks(ctx context.Context, values []string) (TrustedNetworksStatus, error) {
	canonical, normalized, err := trustednetworks.CanonicalList(values)
	if err != nil {
		return TrustedNetworksStatus{}, err
	}

	a.trustedNetworksSettingsMu.Lock()
	defer a.trustedNetworksSettingsMu.Unlock()

	snapshot := a.ConfigSnapshot()
	if snapshot == nil {
		return TrustedNetworksStatus{}, errors.New("trusted-network config is unavailable")
	}
	cur := snapshot.TrustedNetworks
	if err := errIfEnvLockedChanged(trustednetworks.SettingKey, cur, canonical); err != nil {
		return TrustedNetworksStatus{}, err
	}
	if cur.Value == canonical {
		return a.TrustedNetworksStatus(), nil
	}

	_, previous, parseErr := trustednetworks.Canonical(cur.Value)
	if parseErr != nil {
		return TrustedNetworksStatus{}, fmt.Errorf("current trusted-network config is invalid: %w", parseErr)
	}
	manager := a.Ingress()
	if manager != nil {
		if err := manager.SetTrustedNetworks(ctx, normalized); err != nil {
			return TrustedNetworksStatus{}, err
		}
	}

	if cur.Source != config.SourceEnv {
		if err := writeSetting(a, ctx, trustednetworks.SettingKey, normalized); err != nil {
			if manager != nil {
				if rollbackErr := manager.SetTrustedNetworks(ctx, previous); rollbackErr != nil {
					return TrustedNetworksStatus{}, fmt.Errorf("persisting trusted networks: %w (restoring ingress policy: %v)", err, rollbackErr)
				}
			}
			return TrustedNetworksStatus{}, fmt.Errorf("persisting trusted networks: %w", err)
		}
		a.configMu.Lock()
		a.config.TrustedNetworks = config.Field[string]{Value: strings.Join(normalized, ","), Source: config.SourceDB}
		a.configMu.Unlock()
	}
	return a.TrustedNetworksStatus(), nil
}
