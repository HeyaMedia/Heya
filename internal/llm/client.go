package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal OpenAI-compatible chat client. One implementation
// serves every provider — external SaaS, Ollama, and Heya's own managed
// llama-server all speak this protocol.
type Client struct {
	baseURL string // ".../v1"-style root, no trailing slash
	apiKey  string // empty for keyless local servers
	hc      *http.Client
}

// NewClient builds a client for an OpenAI-compatible endpoint. baseURL is the
// API root (e.g. "https://api.openai.com/v1"); apiKey may be empty.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		// Generous ceiling: long completions on slow local hardware are the
		// norm, not the exception. Callers bound individual requests via ctx.
		hc: &http.Client{Timeout: 5 * time.Minute},
	}
}

// --- wire shapes (OpenAI chat completions) -------------------------------

type wireChatRequest struct {
	Model          string          `json:"model,omitempty"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type       string          `json:"type"` // "json_schema"
	JSONSchema *jsonSchemaSpec `json:"json_schema,omitempty"`
}

type jsonSchemaSpec struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type wireChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type wireModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// --- public API -----------------------------------------------------------

// Complete sends one chat-completion request and returns the reply.
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	return c.complete(ctx, wireChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	})
}

// CompleteJSON asks for a reply constrained to the given JSON schema and
// unmarshals it into out. Servers with native json_schema support (llama.cpp,
// OpenAI, most aggregators) enforce it server-side; for the rest we fall back
// to embedding the schema in the prompt and validating client-side, with one
// corrective retry on a malformed reply.
func (c *Client) CompleteJSON(ctx context.Context, req Request, name string, schema json.RawMessage, out any) error {
	wire := wireChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		ResponseFormat: &responseFormat{
			Type:       "json_schema",
			JSONSchema: &jsonSchemaSpec{Name: name, Strict: true, Schema: schema},
		},
	}
	resp, err := c.complete(ctx, wire)
	if isBadRequest(err) {
		// Provider rejects response_format — degrade to prompt-embedded schema.
		wire.ResponseFormat = nil
		wire.Messages = append([]Message{{
			Role: "system",
			Content: "Reply with ONLY a JSON object matching this JSON Schema — no prose, no markdown fences:\n" +
				string(schema),
		}}, req.Messages...)
		resp, err = c.complete(ctx, wire)
	}
	if err != nil {
		return err
	}

	if jsonErr := json.Unmarshal(extractJSON(resp.Content), out); jsonErr != nil {
		// One corrective round-trip: show the model its own error.
		wire.Messages = append(wire.Messages,
			Message{Role: "assistant", Content: resp.Content},
			Message{Role: "user", Content: fmt.Sprintf(
				"That was not valid JSON for the schema (%v). Reply again with ONLY the corrected JSON object.", jsonErr)},
		)
		resp, err = c.complete(ctx, wire)
		if err != nil {
			return err
		}
		if jsonErr := json.Unmarshal(extractJSON(resp.Content), out); jsonErr != nil {
			return fmt.Errorf("llm: reply did not match schema after retry: %w", jsonErr)
		}
	}
	return nil
}

// Models lists model ids from the provider's /models endpoint.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: models request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp.StatusCode, body)
	}
	var parsed wireModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("llm: models response: %w", err)
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Ping verifies the endpoint is reachable and authorized by listing models.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Models(ctx)
	return err
}

// --- internals ------------------------------------------------------------

func (c *Client) complete(ctx context.Context, wire wireChatRequest) (*Response, error) {
	payload, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: chat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp.StatusCode, body)
	}
	var parsed wireChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("llm: chat response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("llm: server error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty choices in response")
	}
	choice := parsed.Choices[0]
	return &Response{
		Content:      choice.Message.Content,
		Model:        parsed.Model,
		FinishReason: choice.FinishReason,
		Usage:        parsed.Usage,
	}, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// StatusError carries the HTTP status of a failed API call so callers can
// distinguish auth failures (401/403) from capability rejections (400).
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("llm: HTTP %d: %s", e.Status, e.Body)
}

func httpError(status int, body []byte) error {
	// Provider error bodies are {"error":{"message":...}} — surface just the
	// message when parseable, the raw (truncated) body otherwise.
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return &StatusError{Status: status, Body: msg}
}

func isBadRequest(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == http.StatusBadRequest
}

// extractJSON strips markdown fences and any prose around the outermost JSON
// value — small local models love decorating their JSON.
func extractJSON(s string) []byte {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return []byte(s)
	}
	end := strings.LastIndexAny(s, "}]")
	if end < start {
		return []byte(s)
	}
	return []byte(s[start : end+1])
}
