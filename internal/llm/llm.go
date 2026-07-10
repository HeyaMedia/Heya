// Package llm is Heya's language-model subsystem. Local and external models
// use an OpenAI-compatible Client; subscription-backed Claude and Codex modes
// use hardened wrappers around the vendors' official native CLIs. Consumers
// depend only on Completer and never care which transport they are using.
package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// Completer is the provider-neutral surface used by Heya's AI features.
// OpenAI-compatible HTTP endpoints and subscription-backed agent CLIs both
// implement it. Tool support deliberately lives above this interface so Heya
// can expose an explicit allowlist instead of inheriting an agent's built-ins.
type Completer interface {
	Complete(context.Context, Request) (*Response, error)
	CompleteJSON(context.Context, Request, string, json.RawMessage, any) error
	Models(context.Context) ([]string, error)
}

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
