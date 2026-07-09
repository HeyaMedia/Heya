package llm

// Provider is one row of the static preset table. The table is deliberately
// hand-maintained data, not a library dependency: every entry is just an
// OpenAI-compatible base URL plus whether it needs a bearer key. Model lists
// come from each provider's /v1/models at runtime, never from here.
type Provider struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	BaseURL  string `json:"base_url"`
	NeedsKey bool   `json:"needs_key"`
}

// ProviderCustom is the escape hatch: user supplies the base URL.
const ProviderCustom = "custom"

// Providers is the preset table shown in the Settings dropdown, in display
// order. Local-network entries (Ollama, LM Studio) point at their default
// ports and need no key.
var Providers = []Provider{
	{ID: "openrouter", Label: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", NeedsKey: true},
	{ID: "openai", Label: "OpenAI", BaseURL: "https://api.openai.com/v1", NeedsKey: true},
	{ID: "anthropic", Label: "Anthropic", BaseURL: "https://api.anthropic.com/v1", NeedsKey: true},
	{ID: "gemini", Label: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", NeedsKey: true},
	{ID: "groq", Label: "Groq", BaseURL: "https://api.groq.com/openai/v1", NeedsKey: true},
	{ID: "mistral", Label: "Mistral", BaseURL: "https://api.mistral.ai/v1", NeedsKey: true},
	{ID: "deepseek", Label: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", NeedsKey: true},
	{ID: "moonshot", Label: "Moonshot (Kimi)", BaseURL: "https://api.moonshot.ai/v1", NeedsKey: true},
	{ID: "zai", Label: "Z.ai (GLM)", BaseURL: "https://api.z.ai/api/paas/v4", NeedsKey: true},
	{ID: "together", Label: "Together AI", BaseURL: "https://api.together.xyz/v1", NeedsKey: true},
	{ID: "fireworks", Label: "Fireworks AI", BaseURL: "https://api.fireworks.ai/inference/v1", NeedsKey: true},
	{ID: "xai", Label: "xAI (Grok)", BaseURL: "https://api.x.ai/v1", NeedsKey: true},
	{ID: "ollama", Label: "Ollama (local network)", BaseURL: "http://127.0.0.1:11434/v1", NeedsKey: false},
	{ID: "lmstudio", Label: "LM Studio (local network)", BaseURL: "http://127.0.0.1:1234/v1", NeedsKey: false},
	{ID: ProviderCustom, Label: "Custom (OpenAI-compatible URL)", BaseURL: "", NeedsKey: false},
}

// ProviderByID looks up a preset. ok=false for unknown ids.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}
