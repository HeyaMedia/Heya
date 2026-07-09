// Package llm is Heya's language-model subsystem. Everything — local and
// external — speaks the OpenAI-compatible chat-completions API through one
// Client. "Local" is a managed llama-server subprocess (llama.cpp) owned by
// LocalRuntime; "external" is any provider from the preset table (or a custom
// base URL). Consumers never care which one they're talking to.
package llm

import "errors"

// Message is one chat turn in the OpenAI wire shape.
type Message struct {
	Role    string `json:"role" doc:"system | user | assistant"`
	Content string `json:"content"`
}

// Request is a chat-completion request. Model may be empty for servers that
// only host one model (llama-server, LM Studio).
type Request struct {
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// Usage is the token accounting reported by the server.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Response is the assistant's reply to a Request.
type Response struct {
	Content      string `json:"content"`
	Model        string `json:"model,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Usage        Usage  `json:"usage"`
}

// ErrNotConfigured is returned when a request needs an LLM but the subsystem
// is off or missing required settings (key, model, base URL).
var ErrNotConfigured = errors.New("llm: not configured")
