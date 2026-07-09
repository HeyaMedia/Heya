package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/karbowiak/heya/internal/llm"
	"github.com/rs/zerolog/log"
)

// ErrAIDisabled is returned by AI entry points when mode=off.
var ErrAIDisabled = errors.New("ai is disabled — enable it in Settings → AI")

// aiClient resolves the effective settings into a ready client. For local
// mode this may spawn (and block on) llama-server startup; ctx bounds that
// wait. The returned model string is what goes into the request ("" for
// single-model local servers).
func (a *App) aiClient(ctx context.Context, s AISettings) (*llm.Client, string, error) {
	switch s.Mode {
	case "", "off":
		return nil, "", ErrAIDisabled
	case "local":
		baseURL, err := a.llmLocal.Ensure(ctx, s.LocalModel, s.LocalBackend, s.ContextSize)
		if err != nil {
			return nil, "", err
		}
		return llm.NewClient(baseURL, ""), "", nil
	case "external":
		baseURL, err := aiExternalBaseURL(s)
		if err != nil {
			return nil, "", err
		}
		if s.Model == "" {
			return nil, "", fmt.Errorf("%w: no model selected", llm.ErrNotConfigured)
		}
		return llm.NewClient(baseURL, s.APIKey), s.Model, nil
	default:
		return nil, "", fmt.Errorf("invalid ai mode %q", s.Mode)
	}
}

func aiExternalBaseURL(s AISettings) (string, error) {
	p, ok := llm.ProviderByID(s.Provider)
	if !ok {
		return "", fmt.Errorf("%w: unknown provider %q", llm.ErrNotConfigured, s.Provider)
	}
	baseURL := p.BaseURL
	if p.ID == llm.ProviderCustom {
		baseURL = s.BaseURL
	}
	if baseURL == "" {
		return "", fmt.Errorf("%w: no base URL", llm.ErrNotConfigured)
	}
	if p.NeedsKey && s.APIKey == "" {
		return "", fmt.Errorf("%w: provider %s needs an API key", llm.ErrNotConfigured, p.Label)
	}
	return baseURL, nil
}

// AIChatRequest is one test-console / consumer chat call. Either Prompt (one
// user turn, with optional System) or Messages (full history) — not both.
type AIChatRequest struct {
	Prompt    string        `json:"prompt,omitempty"`
	System    string        `json:"system,omitempty" doc:"optional system prompt / context"`
	Messages  []llm.Message `json:"messages,omitempty" doc:"full message history; overrides prompt/system"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

// AIChatResponse carries the reply plus enough metadata to debug it.
type AIChatResponse struct {
	Content          string `json:"content"`
	Model            string `json:"model,omitempty"`
	Mode             string `json:"mode"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	DurationMs       int64  `json:"duration_ms"`
}

// AIChat runs one chat completion against whatever the settings point at.
func (a *App) AIChat(ctx context.Context, in AIChatRequest) (AIChatResponse, error) {
	messages := in.Messages
	if len(messages) == 0 {
		if in.Prompt == "" {
			return AIChatResponse{}, fmt.Errorf("empty prompt")
		}
		if in.System != "" {
			messages = append(messages, llm.Message{Role: "system", Content: in.System})
		}
		messages = append(messages, llm.Message{Role: "user", Content: in.Prompt})
	}

	s := a.AISettings(ctx)
	client, model, err := a.aiClient(ctx, s)
	if err != nil {
		return AIChatResponse{}, err
	}

	start := time.Now()
	resp, err := client.Complete(ctx, llm.Request{
		Model:     model,
		Messages:  messages,
		MaxTokens: in.MaxTokens,
	})
	if err != nil {
		return AIChatResponse{}, err
	}
	if s.Mode == "local" {
		a.llmLocal.Touch()
		// llama-server reports the on-disk GGUF path as the model id —
		// surface the catalog id instead.
		resp.Model = s.LocalModel
	}
	return AIChatResponse{
		Content:          resp.Content,
		Model:            resp.Model,
		Mode:             s.Mode,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		DurationMs:       time.Since(start).Milliseconds(),
	}, nil
}

// AICompleteJSON is the schema-constrained entry point consumers (collections
// curator, NL playlists) build on. Same client resolution as AIChat.
func (a *App) AICompleteJSON(ctx context.Context, messages []llm.Message, name string, schema []byte, out any) error {
	s := a.AISettings(ctx)
	client, model, err := a.aiClient(ctx, s)
	if err != nil {
		return err
	}
	err = client.CompleteJSON(ctx, llm.Request{Model: model, Messages: messages}, name, schema, out)
	if s.Mode == "local" {
		a.llmLocal.Touch()
	}
	return err
}

// AIModels lists selectable model ids for the current settings: the
// provider's /v1/models in external mode, the curated catalog in local mode.
func (a *App) AIModels(ctx context.Context) ([]string, error) {
	s := a.AISettings(ctx)
	switch s.Mode {
	case "", "off":
		return nil, ErrAIDisabled
	case "local":
		ids := make([]string, 0, len(llm.LocalModels))
		for _, m := range llm.LocalModels {
			ids = append(ids, m.ID)
		}
		return ids, nil
	default:
		baseURL, err := aiExternalBaseURL(s)
		if err != nil {
			return nil, err
		}
		return llm.NewClient(baseURL, s.APIKey).Models(ctx)
	}
}

// AILocalStatus is the local-runtime slice of AIStatusReport.
type AILocalStatus struct {
	Build            string                `json:"build" doc:"pinned llama.cpp release"`
	ServerPresent    bool                  `json:"server_present"`
	ModelPresent     bool                  `json:"model_present"`
	Running          bool                  `json:"running"`
	RunningModel     string                `json:"running_model,omitempty"`
	DownloadState    string                `json:"download_state"`
	DownloadProgress *llm.DownloadProgress `json:"download_progress,omitempty"`
	DownloadError    string                `json:"download_error,omitempty"`
}

// AIStatusReport is the poll-friendly overview for the Settings page + CLI.
type AIStatusReport struct {
	Mode        string        `json:"mode"`
	Ready       bool          `json:"ready"`
	Detail      string        `json:"detail,omitempty" doc:"human-readable reason when not ready"`
	Provider    string        `json:"provider,omitempty"`
	Model       string        `json:"model,omitempty"`
	LocalModel  string        `json:"local_model,omitempty"`
	ContextSize int           `json:"context_size,omitempty"`
	Local       AILocalStatus `json:"local"`
}

// AIStatus reports whether the subsystem could serve a request right now,
// without actually spawning or calling anything.
func (a *App) AIStatus(ctx context.Context) AIStatusReport {
	s := a.AISettings(ctx)
	dlState, dlProg, dlErr := a.llmLocal.DownloadStatus()
	running, runningModel := a.llmLocal.Running()
	report := AIStatusReport{
		Mode:        s.Mode,
		Provider:    s.Provider,
		Model:       s.Model,
		LocalModel:  s.LocalModel,
		ContextSize: s.ContextSize,
		Local: AILocalStatus{
			Build:            llm.ServerBuild,
			ServerPresent:    a.llmLocal.ServerPresent(s.LocalBackend),
			ModelPresent:     a.llmLocal.ModelPresent(s.LocalModel),
			Running:          running,
			RunningModel:     runningModel,
			DownloadState:    string(dlState),
			DownloadProgress: dlProg,
			DownloadError:    dlErr,
		},
	}

	switch s.Mode {
	case "", "off":
		report.Detail = "disabled"
	case "local":
		switch {
		case !report.Local.ServerPresent && !report.Local.ModelPresent:
			report.Detail = "runtime + model not downloaded"
		case !report.Local.ServerPresent:
			report.Detail = "runtime not downloaded"
		case !report.Local.ModelPresent:
			report.Detail = "model not downloaded"
		default:
			report.Ready = true
		}
	case "external":
		if _, err := aiExternalBaseURL(s); err != nil {
			report.Detail = err.Error()
		} else if s.Model == "" {
			report.Detail = "no model selected"
		} else {
			report.Ready = true
		}
	}
	return report
}

// AIDownloadLocal kicks off the local artifact download in the background
// (bound to app lifetime, like sonic model fetches). Fails fast when one is
// already running or local mode is misconfigured.
func (a *App) AIDownloadLocal(ctx context.Context) error {
	s := a.AISettings(ctx)
	if _, ok := llm.LocalModelByID(s.LocalModel); !ok {
		return fmt.Errorf("unknown local model %q", s.LocalModel)
	}
	if _, err := llm.ServerAssetFor(s.LocalBackend); err != nil {
		return err
	}
	dlCtx := a.LifetimeContext()
	go func() {
		if err := a.llmLocal.Download(dlCtx, s.LocalModel, s.LocalBackend); err != nil {
			log.Err(err).Msg("ai: local artifact download failed")
		}
	}()
	return nil
}

// AIDownloadLocalWait is the CLI-friendly synchronous variant.
func (a *App) AIDownloadLocalWait(ctx context.Context) error {
	s := a.AISettings(ctx)
	return a.llmLocal.Download(ctx, s.LocalModel, s.LocalBackend)
}

// AIStopLocal kills a running llama-server (no-op when idle).
func (a *App) AIStopLocal() { a.llmLocal.Stop() }

// AIProviders exposes the static preset table for the Settings dropdown.
func (a *App) AIProviders() []llm.Provider { return llm.Providers }

// AILocalModels exposes the curated GGUF catalog for the Settings dropdown.
func (a *App) AILocalModels() []llm.LocalModel { return llm.LocalModels }
