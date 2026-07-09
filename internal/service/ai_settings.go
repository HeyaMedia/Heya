package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/llm"
)

// Env vars that overlay the UI-tunable AI fields. When set they win over the
// DB blob and lock the corresponding control in the UI (same contract as
// HEYA_SONIC_*).
const (
	aiEnvMode         = "HEYA_AI_MODE"
	aiEnvProvider     = "HEYA_AI_PROVIDER"
	aiEnvAPIKey       = "HEYA_AI_API_KEY" //nolint:gosec // G101: env var *name*, not a credential
	aiEnvModel        = "HEYA_AI_MODEL"
	aiEnvBaseURL      = "HEYA_AI_BASE_URL"
	aiEnvLocalModel   = "HEYA_AI_LOCAL_MODEL"
	aiEnvLocalBackend = "HEYA_AI_LOCAL_BACKEND"
	aiEnvContext      = "HEYA_AI_CONTEXT"
)

// AISettings is the user-tunable configuration of the AI subsystem, stored as
// one JSON blob in system_settings (key=ai). Mode defaults to "off" so a
// fresh install never spawns processes or phones external APIs.
type AISettings struct {
	Mode         string `json:"mode"`          // off | local | external
	Provider     string `json:"provider"`      // preset id (external mode)
	APIKey       string `json:"api_key"`       // bearer key (external mode)
	Model        string `json:"model"`         // provider model id (external mode)
	BaseURL      string `json:"base_url"`      // custom provider only
	LocalModel   string `json:"local_model"`   // curated catalog id (local mode)
	LocalBackend string `json:"local_backend"` // auto | cpu | vulkan
	ContextSize  int    `json:"context_size"`  // llama-server --ctx-size
}

// DefaultAISettings returns the fallback applied when no system_settings row
// exists yet. The modest default context keeps KV-cache RAM in check on
// low-power boxes; the knob goes up to 131072 for those with headroom.
func DefaultAISettings() AISettings {
	return AISettings{
		Mode:         "off",
		Provider:     "openrouter",
		LocalModel:   llm.DefaultLocalModel,
		LocalBackend: llm.BackendAuto,
		ContextSize:  16384,
	}
}

const aiSettingsKey = "ai"

func aiStringFromEnv(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

func aiIntFromEnv(name string) (int, bool) {
	v, ok := aiStringFromEnv(name)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// AISettings reads the persisted settings with env overlay — env wins per
// field, exactly like the sonic-analysis contract.
func (a *App) AISettings(ctx context.Context) AISettings {
	s := a.aiSettingsFromDB(ctx)
	if v, ok := aiStringFromEnv(aiEnvMode); ok {
		s.Mode = v
	}
	if v, ok := aiStringFromEnv(aiEnvProvider); ok {
		s.Provider = v
	}
	if v, ok := aiStringFromEnv(aiEnvAPIKey); ok {
		s.APIKey = v
	}
	if v, ok := aiStringFromEnv(aiEnvModel); ok {
		s.Model = v
	}
	if v, ok := aiStringFromEnv(aiEnvBaseURL); ok {
		s.BaseURL = v
	}
	if v, ok := aiStringFromEnv(aiEnvLocalModel); ok {
		s.LocalModel = v
	}
	if v, ok := aiStringFromEnv(aiEnvLocalBackend); ok {
		s.LocalBackend = v
	}
	if v, ok := aiIntFromEnv(aiEnvContext); ok {
		s.ContextSize = v
	}
	return s
}

func (a *App) aiSettingsFromDB(ctx context.Context) AISettings {
	s := DefaultAISettings()
	raw, err := a.GetSystemSetting(ctx, aiSettingsKey)
	if err == nil {
		var persisted AISettings
		if json.Unmarshal(raw, &persisted) == nil {
			s = persisted
			// Backfill zero-values so older blobs pick up new defaults.
			d := DefaultAISettings()
			if s.Mode == "" {
				s.Mode = d.Mode
			}
			if s.Provider == "" {
				s.Provider = d.Provider
			}
			if s.LocalModel == "" {
				s.LocalModel = d.LocalModel
			}
			if s.LocalBackend == "" {
				s.LocalBackend = d.LocalBackend
			}
			if s.ContextSize == 0 {
				s.ContextSize = d.ContextSize
			}
		}
	}
	return s
}

// aiEnvLocks maps settings-field name → env var, for fields currently locked
// by env. Keys match the dotted `ai.<field>` names in /api/config/sources.
func aiEnvLocks() map[string]string {
	locks := map[string]string{}
	check := func(field, envVar string) {
		if _, ok := aiStringFromEnv(envVar); ok {
			locks[field] = envVar
		}
	}
	check("mode", aiEnvMode)
	check("provider", aiEnvProvider)
	check("api_key", aiEnvAPIKey)
	check("model", aiEnvModel)
	check("base_url", aiEnvBaseURL)
	check("local_model", aiEnvLocalModel)
	check("local_backend", aiEnvLocalBackend)
	if _, ok := aiIntFromEnv(aiEnvContext); ok {
		locks["context_size"] = aiEnvContext
	}
	return locks
}

// SetAISettings validates and persists new settings. An empty APIKey means
// "keep the stored key" so the UI never has to echo secrets back. Env-locked
// fields refuse a *changed* value and silently keep the DB row otherwise.
func (a *App) SetAISettings(ctx context.Context, s AISettings) error {
	switch s.Mode {
	case "off", "local", "external":
	default:
		return fmt.Errorf("invalid mode %q (off|local|external)", s.Mode)
	}
	switch s.LocalBackend {
	case llm.BackendAuto, llm.BackendCPU, llm.BackendVulkan:
	default:
		return fmt.Errorf("invalid local_backend %q (auto|cpu|vulkan)", s.LocalBackend)
	}
	if s.Provider != "" {
		if _, ok := llm.ProviderByID(s.Provider); !ok {
			return fmt.Errorf("unknown provider %q", s.Provider)
		}
	}
	if s.LocalModel != "" {
		if _, ok := llm.LocalModelByID(s.LocalModel); !ok {
			return fmt.Errorf("unknown local model %q", s.LocalModel)
		}
	}
	if s.ContextSize != 0 && (s.ContextSize < 1024 || s.ContextSize > 131072) {
		return fmt.Errorf("context_size %d out of range (1024–131072)", s.ContextSize)
	}

	persisted := a.aiSettingsFromDB(ctx)
	if s.APIKey == "" {
		s.APIKey = persisted.APIKey
	}

	// Validate-all-then-write-all: refuse changes to env-locked fields.
	effective := a.AISettings(ctx)
	type lockCheck struct {
		field   string
		envVar  string
		changed bool
	}
	checks := []lockCheck{
		{"mode", aiEnvMode, s.Mode != effective.Mode},
		{"provider", aiEnvProvider, s.Provider != effective.Provider},
		{"api_key", aiEnvAPIKey, s.APIKey != effective.APIKey},
		{"model", aiEnvModel, s.Model != effective.Model},
		{"base_url", aiEnvBaseURL, s.BaseURL != effective.BaseURL},
		{"local_model", aiEnvLocalModel, s.LocalModel != effective.LocalModel},
		{"local_backend", aiEnvLocalBackend, s.LocalBackend != effective.LocalBackend},
		{"context_size", aiEnvContext, s.ContextSize != effective.ContextSize},
	}
	locks := aiEnvLocks()
	for _, c := range checks {
		if _, locked := locks[c.field]; locked && c.changed {
			return &ErrFieldLockedByEnv{Field: "ai." + c.field, EnvVar: c.envVar}
		}
	}

	// Persist only DB-owned fields: env-locked ones keep their stored value
	// so removing the env var later reveals the previous UI choice.
	out := s
	if _, locked := locks["mode"]; locked {
		out.Mode = persisted.Mode
	}
	if _, locked := locks["provider"]; locked {
		out.Provider = persisted.Provider
	}
	if _, locked := locks["api_key"]; locked {
		out.APIKey = persisted.APIKey
	}
	if _, locked := locks["model"]; locked {
		out.Model = persisted.Model
	}
	if _, locked := locks["base_url"]; locked {
		out.BaseURL = persisted.BaseURL
	}
	if _, locked := locks["local_model"]; locked {
		out.LocalModel = persisted.LocalModel
	}
	if _, locked := locks["local_backend"]; locked {
		out.LocalBackend = persisted.LocalBackend
	}
	if _, locked := locks["context_size"]; locked {
		out.ContextSize = persisted.ContextSize
	}

	// Persisting the key server-side is the point of this blob — it is never
	// echoed to clients (GET goes through AISettingsView, which redacts).
	buf, err := json.Marshal(out) //nolint:gosec // G117: intentional secret-at-rest in system_settings
	if err != nil {
		return err
	}
	return a.SetSystemSetting(ctx, aiSettingsKey, buf)
}

// AISettingsView is the API-safe projection of AISettings: the key never
// leaves the server, only its presence and a short hint.
type AISettingsView struct {
	Mode         string `json:"mode"`
	Provider     string `json:"provider"`
	APIKeySet    bool   `json:"api_key_set"`
	APIKeyHint   string `json:"api_key_hint,omitempty" doc:"last 4 characters, for recognition only"`
	Model        string `json:"model"`
	BaseURL      string `json:"base_url"`
	LocalModel   string `json:"local_model"`
	LocalBackend string `json:"local_backend"`
	ContextSize  int    `json:"context_size"`
}

// AISettingsForAPI returns the redacted settings view for the settings UI.
func (a *App) AISettingsForAPI(ctx context.Context) AISettingsView {
	s := a.AISettings(ctx)
	hint := ""
	if n := len(s.APIKey); n >= 8 {
		hint = "…" + s.APIKey[n-4:]
	}
	return AISettingsView{
		Mode:         s.Mode,
		Provider:     s.Provider,
		APIKeySet:    s.APIKey != "",
		APIKeyHint:   hint,
		Model:        s.Model,
		BaseURL:      s.BaseURL,
		LocalModel:   s.LocalModel,
		LocalBackend: s.LocalBackend,
		ContextSize:  s.ContextSize,
	}
}
