package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"name":  {"type": "string"},
		"kind":  {"type": "string", "enum": ["rules", "pinned"]},
		"count": {"type": "integer"}
	},
	"required": ["name", "kind"],
	"additionalProperties": false
}`)

type testOut struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// chatStub serves /chat/completions, replying with each canned content in
// order and recording the request messages it saw.
func chatStub(t *testing.T, replies ...string) (*httptest.Server, *[][]Message) {
	t.Helper()
	var calls [][]Message
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		var req wireChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		calls = append(calls, req.Messages)
		if i >= len(replies) {
			t.Errorf("unexpected extra request %d", i)
		}
		content := replies[min(i, len(replies)-1)]
		i++
		resp := map[string]any{
			"model": "stub",
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return srv, &calls
}

func TestCompleteJSONValid(t *testing.T) {
	srv, calls := chatStub(t, `{"name": "80s Action", "kind": "rules", "count": 3}`)
	defer srv.Close()

	var out testOut
	err := NewClient(srv.URL, "").CompleteJSON(context.Background(),
		Request{Messages: []Message{{Role: "user", Content: "go"}}}, "collection", testSchema, &out)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if out.Name != "80s Action" || out.Kind != "rules" || out.Count != 3 {
		t.Fatalf("unexpected out: %+v", out)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*calls))
	}
}

func TestCompleteJSONSchemaViolationRetries(t *testing.T) {
	// First reply unmarshals fine into testOut (missing required "kind",
	// bogus enum would too) — the old unmarshal-only check would have
	// accepted it. Schema validation must reject it and retry.
	srv, calls := chatStub(t,
		`{"name": "80s Action"}`,
		"```json\n{\"name\": \"80s Action\", \"kind\": \"pinned\"}\n```",
	)
	defer srv.Close()

	var out testOut
	err := NewClient(srv.URL, "").CompleteJSON(context.Background(),
		Request{Messages: []Message{{Role: "user", Content: "go"}}}, "collection", testSchema, &out)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if out.Kind != "pinned" {
		t.Fatalf("expected corrected reply, got %+v", out)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(*calls))
	}
	// The corrective prompt must name the violation so the model can fix it.
	last := (*calls)[1]
	corrective := last[len(last)-1].Content
	if !strings.Contains(corrective, "kind") {
		t.Fatalf("corrective prompt does not mention the violated field: %q", corrective)
	}
}

func TestCompleteJSONPersistentViolationFails(t *testing.T) {
	srv, _ := chatStub(t,
		`{"name": "x", "kind": "nonsense"}`,
		`{"name": "x", "kind": "still-nonsense", "extra": true}`,
	)
	defer srv.Close()

	var out testOut
	err := NewClient(srv.URL, "").CompleteJSON(context.Background(),
		Request{Messages: []Message{{Role: "user", Content: "go"}}}, "collection", testSchema, &out)
	if err == nil {
		t.Fatal("expected schema violation error, got nil")
	}
	if !strings.Contains(err.Error(), "schema") {
		t.Fatalf("error should mention schema: %v", err)
	}
}

func TestCompleteJSONFallbackWhenResponseFormatRejected(t *testing.T) {
	// First request (with response_format) → 400; the retry without it must
	// carry the schema in a system message and still be validated.
	var sawFallback bool
	first := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wireChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if first {
			first = false
			if req.ResponseFormat == nil {
				t.Error("first request should carry response_format")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error": {"message": "response_format is not supported"}}`)
			return
		}
		if req.ResponseFormat != nil {
			t.Error("fallback request should not carry response_format")
		}
		if len(req.Messages) == 0 || !strings.Contains(req.Messages[0].Content, "JSON Schema") {
			t.Error("fallback request should embed the schema in a system message")
		}
		sawFallback = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "stub",
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": `{"name": "n", "kind": "rules"}`}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	var out testOut
	err := NewClient(srv.URL, "").CompleteJSON(context.Background(),
		Request{Messages: []Message{{Role: "user", Content: "go"}}}, "collection", testSchema, &out)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if !sawFallback || out.Kind != "rules" {
		t.Fatalf("fallback path not exercised correctly: %+v", out)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":  `{"a":1}`,
		"Here you go: {\"a\":1} !": `{"a":1}`,
		"[1,2,3]":                  `[1,2,3]`,
		"```\n[{\"a\":1}]\n```":    `[{"a":1}]`,
	}
	for in, want := range cases {
		if got := string(extractJSON(in)); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
