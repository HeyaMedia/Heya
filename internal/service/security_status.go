package service

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/securityevents"
)

const embeddedCRSModule = "github.com/corazawaf/coraza-coreruleset/v4"

type SecurityConfigStatus struct {
	Value           string        `json:"value"`
	Source          config.Source `json:"source"`
	EnvVar          string        `json:"env_var,omitempty"`
	RestartRequired bool          `json:"restart_required"`
}

type RegistrationSecurityStatus struct {
	SecurityConfigStatus
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	State     string `json:"state" enum:"disabled,available,closed,unknown"`
}

type WAFSecurityStatus struct {
	SecurityConfigStatus
	Enabled         bool   `json:"enabled"`
	Blocking        bool   `json:"blocking"`
	CRSVersion      string `json:"crs_version"`
	RulesBundled    bool   `json:"rules_bundled"`
	UpdatedWithHeya bool   `json:"updated_with_heya"`
}

type LoginRatePolicy struct {
	Burst         int `json:"burst"`
	RefillSeconds int `json:"refill_seconds"`
}

type LoginProtectionStatus struct {
	ByIP               LoginRatePolicy      `json:"by_ip"`
	ByAccount          LoginRatePolicy      `json:"by_account"`
	TrackedKeyCapacity int                  `json:"tracked_key_capacity"`
	Stats              auth.LoginGuardStats `json:"stats"`
}

type PasswordSecurityStatus struct {
	MinimumLength            int    `json:"minimum_length"`
	MaximumLength            int    `json:"maximum_length"`
	HashAlgorithm            string `json:"hash_algorithm"`
	LegacyHashesUpgraded     bool   `json:"legacy_hashes_upgraded"`
	UnknownUserTimingDefense bool   `json:"unknown_user_timing_defense"`
	PasswordChangeRevokes    bool   `json:"password_change_revokes_other_credentials"`
}

type HTTPSecurityStatus struct {
	SecurityHeaders         bool   `json:"security_headers"`
	CSPMode                 string `json:"csp_mode" enum:"report-only,enforced"`
	SameOriginCSRFGate      bool   `json:"same_origin_csrf_gate"`
	HSTSOnPublicIngress     bool   `json:"hsts_on_public_ingress"`
	ApplicationBodyLimit    int64  `json:"application_body_limit_bytes"`
	TrustedForwardedHeaders bool   `json:"trusted_forwarded_headers"`
}

type SecurityStatus struct {
	GeneratedAt  time.Time                            `json:"generated_at"`
	StartedAt    time.Time                            `json:"started_at,omitempty"`
	Registration RegistrationSecurityStatus           `json:"registration"`
	WAF          WAFSecurityStatus                    `json:"waf"`
	Login        LoginProtectionStatus                `json:"login"`
	Password     PasswordSecurityStatus               `json:"password"`
	HTTP         HTTPSecurityStatus                   `json:"http"`
	Events       securityevents.SecurityEventSnapshot `json:"events"`
}

// SecurityStatus returns a read-only view of the controls that protect the
// public boundary. Runtime counters and recent events reset when the API
// process restarts; effective boot configuration retains exact provenance.
func (a *App) SecurityStatus(ctx context.Context) SecurityStatus {
	status := SecurityStatus{
		GeneratedAt: time.Now().UTC(),
		Registration: RegistrationSecurityStatus{
			SecurityConfigStatus: SecurityConfigStatus{Value: "false", Source: config.SourceDefault, RestartRequired: true},
			State:                "disabled",
		},
		WAF: WAFSecurityStatus{
			SecurityConfigStatus: SecurityConfigStatus{Value: "off", Source: config.SourceDefault, RestartRequired: true},
			CRSVersion:           dependencyVersion(embeddedCRSModule),
			RulesBundled:         true,
			UpdatedWithHeya:      true,
		},
		Login: LoginProtectionStatus{
			ByIP:               LoginRatePolicy{Burst: auth.LoginIPBurst, RefillSeconds: auth.LoginIPRefillSeconds},
			ByAccount:          LoginRatePolicy{Burst: auth.LoginAccountBurst, RefillSeconds: auth.LoginAccountRefillSeconds},
			TrackedKeyCapacity: auth.LoginTrackedKeyCapacity,
		},
		Password: PasswordSecurityStatus{
			MinimumLength: auth.MinPasswordLength, MaximumLength: auth.MaxPasswordLength,
			HashAlgorithm: "Argon2id (19 MiB, 2 iterations)", LegacyHashesUpgraded: true,
			UnknownUserTimingDefense: true, PasswordChangeRevokes: true,
		},
		HTTP: HTTPSecurityStatus{
			SecurityHeaders: true, CSPMode: "report-only", SameOriginCSRFGate: true,
			HSTSOnPublicIngress: true, ApplicationBodyLimit: 1 << 20,
			TrustedForwardedHeaders: false,
		},
	}
	if a == nil {
		status.Events = securityevents.SecurityEventSnapshot{Recent: []securityevents.SecurityEvent{}}
		return status
	}
	status.StartedAt = a.StartedAt()
	status.Login.Stats = a.LoginGuard().Stats()
	status.Events = a.SecurityEvents().Snapshot(50)

	if cfg := a.ConfigSnapshot(); cfg != nil {
		registrationSource := normalizedSource(cfg.EnableRegistration.Source)
		status.Registration.SecurityConfigStatus = SecurityConfigStatus{
			Value: boolText(cfg.EnableRegistration.Value), Source: registrationSource,
			EnvVar: cfg.EnableRegistration.EnvVar, RestartRequired: true,
		}
		status.Registration.Enabled = cfg.EnableRegistration.Value
		if cfg.EnableRegistration.Value {
			status.Registration.State = "unknown"
			if a.DBPool() != nil {
				if available, err := a.RegistrationAvailable(ctx); err == nil {
					status.Registration.Available = available
					if available {
						status.Registration.State = "available"
					} else {
						status.Registration.State = "closed"
					}
				}
			}
		}

		mode := cfg.WAFMode.Value
		if mode == "" {
			mode = "off"
		}
		status.WAF.SecurityConfigStatus = SecurityConfigStatus{
			Value: mode, Source: normalizedSource(cfg.WAFMode.Source),
			EnvVar: cfg.WAFMode.EnvVar, RestartRequired: true,
		}
		status.WAF.Enabled = mode == "detect" || mode == "block"
		status.WAF.Blocking = mode == "block"
	}
	return status
}

func normalizedSource(source config.Source) config.Source {
	if source == "" {
		return config.SourceDefault
	}
	return source
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func dependencyVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "embedded"
	}
	for _, dependency := range info.Deps {
		if dependency.Path != path {
			continue
		}
		if dependency.Replace != nil && dependency.Replace.Version != "" {
			return dependency.Replace.Version
		}
		if dependency.Version != "" && dependency.Version != "(devel)" {
			return dependency.Version
		}
		return "embedded"
	}
	return "embedded"
}
