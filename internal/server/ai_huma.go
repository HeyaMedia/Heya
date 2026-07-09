package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/llm"
	"github.com/karbowiak/heya/internal/service"
)

// /api/ai/* — the AI subsystem: settings, status, provider/model catalogs,
// the local llama-server runtime, and a chat entry point used by the Settings
// test console. All admin-gated: configuring providers and spending tokens is
// an operator concern. User-facing AI features get their own scoped routes.
func registerAIRoutes(api huma.API, app *service.App) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/ai/status", "get-ai-status", "AI subsystem status", "AI")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.AIStatusReport], error) {
			return noStoreJSON(app.AIStatus(ctx)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/ai/settings", "get-ai-settings", "AI settings", "AI")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.AISettingsView], error) {
			return noStoreJSON(app.AISettingsForAPI(ctx)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/ai/settings", "set-ai-settings", "Update AI settings", "AI")),
		func(ctx context.Context, in *struct{ Body service.AISettings }) (*JSONOutput[service.AISettingsView], error) {
			if err := app.SetAISettings(ctx, in.Body); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(app.AISettingsForAPI(ctx)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/ai/catalog", "get-ai-catalog", "AI provider presets + local model catalog", "AI")),
		func(_ context.Context, _ *struct{}) (*JSONOutput[aiCatalogBody], error) {
			return cachedJSON(aiCatalogBody{
				Providers:   app.AIProviders(),
				LocalModels: app.AILocalModels(),
			}, 300), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/ai/models", "get-ai-models", "List selectable models for the active provider", "AI")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[aiModelsBody], error) {
			models, err := app.AIModels(ctx)
			if err != nil {
				return nil, aiError(err)
			}
			return noStoreJSON(aiModelsBody{Models: models}), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/ai/chat", "post-ai-chat", "Run one chat completion", "AI")),
		func(ctx context.Context, in *struct{ Body service.AIChatRequest }) (*JSONOutput[service.AIChatResponse], error) {
			resp, err := app.AIChat(ctx, in.Body)
			if err != nil {
				return nil, aiError(err)
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/ai/local/download", "post-ai-local-download", "Download local runtime artifacts", "AI")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			if err := app.AIDownloadLocal(ctx); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return statusOK("downloading"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/ai/local/stop", "post-ai-local-stop", "Stop the local llama-server", "AI")),
		func(_ context.Context, _ *struct{}) (*StatusOutput, error) {
			app.AIStopLocal()
			return statusOK("stopped"), nil
		})
}

type aiCatalogBody struct {
	Providers   []llm.Provider   `json:"providers"`
	LocalModels []llm.LocalModel `json:"local_models"`
}

type aiModelsBody struct {
	Models []string `json:"models"`
}

// aiError maps AI-subsystem failures onto helpful HTTP statuses: disabled /
// unconfigured → 409 (fix your settings), upstream provider auth failures →
// 502 with the provider's message, everything else → 500.
func aiError(err error) error {
	var se *llm.StatusError
	switch {
	case errors.Is(err, service.ErrAIDisabled), errors.Is(err, llm.ErrNotConfigured):
		return huma.Error409Conflict(err.Error())
	case errors.As(err, &se):
		return huma.Error502BadGateway(err.Error())
	default:
		return humaServiceErrorStatus(err, http.StatusInternalServerError)
	}
}
