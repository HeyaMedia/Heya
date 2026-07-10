package llm

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestClaudeAgentUsesStructuredToolFreeInvocation(t *testing.T) {
	dir := t.TempDir()
	bin := writeAgentScript(t, dir, "claude", `#!/bin/sh
if env | grep -q '^HEYA_TEST_SECRET='; then
  echo 'parent environment leaked' >&2
  exit 9
fi
previous=''
tools_disabled=0
for argument in "$@"; do
  if [ "$previous" = '--tools' ] && [ -z "$argument" ]; then tools_disabled=1; fi
  previous="$argument"
done
if [ "$tools_disabled" -ne 1 ]; then
  echo 'tools were not disabled' >&2
  exit 8
fi
cat >/dev/null
printf '%s' '{"result":"ignored","structured_output":{"title":"Borg Core"},"usage":{"input_tokens":12,"output_tokens":4}}'
`)
	t.Setenv("HEYA_TEST_SECRET", "must-not-leak")
	client := NewAgentClient(AgentConfig{
		Provider: AgentClaude, Binary: bin, HomeDir: filepath.Join(dir, "home"), OAuthToken: "token",
	})

	var got struct {
		Title string `json:"title"`
	}
	err := client.CompleteJSON(context.Background(), Request{
		Model: "sonnet", Messages: []Message{{Role: "user", Content: "build a mix"}},
	}, "mix", []byte(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`), &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Borg Core" {
		t.Fatalf("title = %q", got.Title)
	}
}

func TestCodexAgentCompletesAndListsDynamicModels(t *testing.T) {
	dir := t.TempDir()
	bin := writeAgentScript(t, dir, "codex", `#!/bin/sh
if [ "$1" = 'app-server' ]; then
  while IFS= read -r line; do
    case "$line" in
      *'"method":"initialize"'*) printf '%s\n' '{"id":1,"result":{}}' ;;
      *'"method":"model/list"'*) printf '%s\n' '{"id":2,"result":{"data":[{"id":"gpt-5.5","model":"gpt-5.5","hidden":false},{"id":"hidden","model":"hidden","hidden":true},{"id":"gpt-5.4-mini","model":"gpt-5.4-mini","hidden":false}]}}' ;;
    esac
  done
  exit 0
fi
output=''
previous=''
shell_disabled=0
for argument in "$@"; do
  if [ "$previous" = '--output-last-message' ]; then output="$argument"; fi
  if [ "$previous" = '--disable' ] && [ "$argument" = 'shell_tool' ]; then shell_disabled=1; fi
  previous="$argument"
done
if [ "$shell_disabled" -ne 1 ]; then
  echo 'shell tool was not disabled' >&2
  exit 7
fi
cat >/dev/null
printf '%s' 'The mix is ready.' >"$output"
`)
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewAgentClient(AgentConfig{Provider: AgentCodex, Binary: bin, HomeDir: home})

	resp, err := client.Complete(context.Background(), Request{
		Model: "gpt-5.5", Messages: []Message{{Role: "user", Content: "build a mix"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "The mix is ready." {
		t.Fatalf("content = %q", resp.Content)
	}
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(models, []string{"gpt-5.5", "gpt-5.4-mini"}) {
		t.Fatalf("models = %#v", models)
	}
}

func TestAgentPromptSeparatesSystemAndConversation(t *testing.T) {
	system, prompt, err := agentPrompt([]Message{
		{Role: "system", Content: "Return a playlist."},
		{Role: "user", Content: "Borg battle music"},
	}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "Return a playlist.") || strings.Contains(prompt, "Return a playlist.") {
		t.Fatalf("system=%q prompt=%q", system, prompt)
	}
	if !strings.Contains(prompt, "Borg battle music") || !strings.Contains(prompt, "500 tokens") {
		t.Fatalf("prompt=%q", prompt)
	}
}

func writeAgentScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
