package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AgentProvider identifies an official subscription-backed agent runtime.
type AgentProvider string

const (
	AgentClaude AgentProvider = "claude"
	AgentCodex  AgentProvider = "codex"
)

// AgentConfig describes one official CLI. HomeDir is private Heya-owned state
// beneath the data directory, not the service account's real home directory.
type AgentConfig struct {
	Provider    AgentProvider
	Binary      string
	HomeDir     string
	ProcessHome string
	ConfigDir   string
	OAuthToken  string // Claude setup-token output; empty for Codex device auth.
	// UseSystemAuth is development-only: use the developer's normal CLI
	// credential home/keychain instead of Heya's isolated persisted login.
	UseSystemAuth bool
}

// AgentClient is a small Go equivalent of the vendors' TypeScript SDK
// wrappers: it launches the official native CLI and exchanges structured
// input/output. No shell is involved and the child receives a minimal env.
type AgentClient struct {
	cfg AgentConfig
}

func NewAgentClient(cfg AgentConfig) *AgentClient { return &AgentClient{cfg: cfg} }

func (c *AgentClient) Complete(ctx context.Context, req Request) (*Response, error) {
	return c.complete(ctx, req, nil)
}

func (c *AgentClient) CompleteJSON(
	ctx context.Context,
	req Request,
	_ string,
	schema json.RawMessage,
	out any,
) error {
	validator, err := newSchemaValidator(schema)
	if err != nil {
		return err
	}

	resp, err := c.complete(ctx, req, schema)
	if err != nil {
		return err
	}
	raw := extractJSON(resp.Content)
	if verr := validator.validate(raw); verr != nil {
		req.Messages = append(req.Messages,
			Message{Role: "assistant", Content: resp.Content},
			Message{Role: "user", Content: fmt.Sprintf(
				"That reply violates the required JSON Schema: %v. Reply again with only the corrected JSON object.", verr)},
		)
		resp, err = c.complete(ctx, req, schema)
		if err != nil {
			return err
		}
		raw = extractJSON(resp.Content)
		if verr := validator.validate(raw); verr != nil {
			return fmt.Errorf("llm: agent reply violates schema after retry: %w", verr)
		}
	}
	return json.Unmarshal(raw, out)
}

func (c *AgentClient) Models(ctx context.Context) ([]string, error) {
	switch c.cfg.Provider {
	case AgentClaude:
		// These aliases intentionally track the current model behind each Claude
		// tier. The model field remains free-form for exact/new identifiers.
		return []string{"fable", "sonnet", "haiku", "opus"}, nil
	case AgentCodex:
		return c.codexModels(ctx)
	default:
		return nil, fmt.Errorf("llm: unknown agent provider %q", c.cfg.Provider)
	}
}

// BinaryPresent reports whether the configured official CLI can be resolved.
func (c *AgentClient) BinaryPresent() bool {
	_, err := c.binaryPath()
	return err == nil
}

// Authenticated is a non-network readiness check suitable for status polling.
func (c *AgentClient) Authenticated() bool {
	switch c.cfg.Provider {
	case AgentClaude:
		return strings.TrimSpace(c.cfg.OAuthToken) != "" || c.cfg.UseSystemAuth
	case AgentCodex:
		st, err := os.Stat(filepath.Join(c.cfg.HomeDir, "auth.json"))
		return err == nil && !st.IsDir() && st.Size() > 0
	default:
		return false
	}
}

func (c *AgentClient) complete(ctx context.Context, req Request, schema json.RawMessage) (*Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, fmt.Errorf("%w: no agent model selected", ErrNotConfigured)
	}
	if !c.Authenticated() {
		return nil, fmt.Errorf("%w: %s subscription is not authenticated", ErrNotConfigured, c.cfg.Provider)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	switch c.cfg.Provider {
	case AgentClaude:
		return c.completeClaude(ctx, req, schema)
	case AgentCodex:
		return c.completeCodex(ctx, req, schema)
	default:
		return nil, fmt.Errorf("llm: unknown agent provider %q", c.cfg.Provider)
	}
}

func (c *AgentClient) completeClaude(ctx context.Context, req Request, schema json.RawMessage) (*Response, error) {
	bin, err := c.binaryPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.cfg.HomeDir, 0o700); err != nil {
		return nil, fmt.Errorf("llm: create Claude home: %w", err)
	}
	workDir, cleanup, err := agentWorkDir("claude")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	system, prompt, err := agentPrompt(req.Messages, req.MaxTokens)
	if err != nil {
		return nil, err
	}
	args := []string{
		"--print",
		"--output-format", "json",
		"--model", req.Model,
		"--tools", "",
		"--permission-mode", "dontAsk",
		"--safe-mode",
		"--disable-slash-commands",
		"--no-chrome",
		"--strict-mcp-config",
		"--mcp-config", `{"mcpServers":{}}`,
		"--setting-sources", "",
		"--no-session-persistence",
		"--system-prompt", system,
	}
	if len(schema) > 0 {
		args = append(args, "--json-schema", string(schema))
	}

	var result struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		Usage            struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	stdout, stderr, runErr := runAgentCommand(ctx, bin, args, prompt, workDir, c.env(workDir))
	decodeErr := json.Unmarshal(stdout, &result)
	if runErr != nil {
		if decodeErr == nil && strings.TrimSpace(result.Result) != "" {
			return nil, fmt.Errorf("llm: Claude failed: %s", safeOutput(result.Result))
		}
		return nil, commandError("Claude", runErr, append(stderr, stdout...))
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("llm: decode Claude result: %w", decodeErr)
	}
	if result.IsError {
		return nil, fmt.Errorf("llm: Claude returned an error: %s", safeOutput(result.Result))
	}
	content := result.Result
	if len(result.StructuredOutput) > 0 && string(result.StructuredOutput) != "null" {
		content = string(result.StructuredOutput)
	}
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("llm: Claude returned an empty result")
	}
	return &Response{
		Content: content,
		Model:   req.Model,
		Usage: Usage{
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
		},
	}, nil
}

func (c *AgentClient) completeCodex(ctx context.Context, req Request, schema json.RawMessage) (*Response, error) {
	bin, err := c.binaryPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.cfg.HomeDir, 0o700); err != nil {
		return nil, fmt.Errorf("llm: create Codex home: %w", err)
	}
	workDir, cleanup, err := agentWorkDir("codex")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	_, prompt, err := agentPrompt(req.Messages, req.MaxTokens)
	if err != nil {
		return nil, err
	}
	outputPath := filepath.Join(workDir, "response.txt")
	args := []string{
		"exec",
		"--strict-config",
		"--model", req.Model,
		"--sandbox", "read-only",
		"--disable", "shell_tool",
		"--disable", "unified_exec",
		"--disable", "standalone_web_search",
		"--disable", "search_tool",
		"--disable", "apps",
		"--disable", "browser_use",
		"--disable", "computer_use",
		"--disable", "multi_agent",
		"--disable", "tool_search",
		"--disable", "code_mode",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--skip-git-repo-check",
		"--color", "never",
		"--output-last-message", outputPath,
		"--cd", workDir,
		"--config", `approval_policy="never"`,
		"--config", `shell_environment_policy.inherit="none"`,
		"-",
	}
	if len(schema) > 0 {
		schemaPath := filepath.Join(workDir, "response.schema.json")
		if err := os.WriteFile(schemaPath, schema, 0o600); err != nil {
			return nil, fmt.Errorf("llm: write Codex response schema: %w", err)
		}
		args = append(args[:len(args)-1], "--output-schema", schemaPath, "-")
	}

	_, stderr, err := runAgentCommand(ctx, bin, args, prompt, workDir, c.env(workDir))
	if err != nil {
		return nil, commandError("Codex", err, stderr)
	}
	raw, err := os.ReadFile(outputPath) //nolint:gosec // path is inside Heya's private temporary directory
	if err != nil {
		return nil, fmt.Errorf("llm: read Codex result: %w", err)
	}
	if len(raw) > 16<<20 {
		return nil, errors.New("llm: Codex result exceeds 16 MiB")
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return nil, errors.New("llm: Codex returned an empty result")
	}
	return &Response{Content: content, Model: req.Model}, nil
}

func (c *AgentClient) codexModels(ctx context.Context) ([]string, error) {
	bin, err := c.binaryPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(c.cfg.HomeDir, 0o700); err != nil {
		return nil, err
	}
	workDir, cleanup, err := agentWorkDir("codex-models")
	if err != nil {
		return nil, err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "app-server", "--stdio") //nolint:gosec // fixed official binary + arguments
	cmd.Dir = workDir
	cmd.Env = c.env(workDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("llm: start Codex app-server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	enc := json.NewEncoder(stdin)
	dec := json.NewDecoder(bufio.NewReader(stdout))
	if err := enc.Encode(map[string]any{
		"id": 1, "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "heya", "title": "Heya", "version": "1"},
			"capabilities": map[string]any{"experimentalApi": true},
		},
	}); err != nil {
		return nil, err
	}
	if _, err := readRPCResult(dec, 1); err != nil {
		return nil, commandError("Codex app-server initialize", err, stderr.Bytes())
	}
	if err := enc.Encode(map[string]any{"method": "initialized"}); err != nil {
		return nil, err
	}
	if err := enc.Encode(map[string]any{
		"id": 2, "method": "model/list",
		"params": map[string]any{"includeHidden": false, "limit": 1000},
	}); err != nil {
		return nil, err
	}
	rawResult, err := readRPCResult(dec, 2)
	if err != nil {
		return nil, commandError("Codex model list", err, stderr.Bytes())
	}
	var page struct {
		Data []struct {
			ID     string `json:"id"`
			Model  string `json:"model"`
			Hidden bool   `json:"hidden"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawResult, &page); err != nil {
		return nil, fmt.Errorf("llm: decode Codex models: %w", err)
	}
	seen := map[string]struct{}{}
	models := make([]string, 0, len(page.Data))
	for _, model := range page.Data {
		id := strings.TrimSpace(model.Model)
		if id == "" {
			id = strings.TrimSpace(model.ID)
		}
		if model.Hidden || id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	if len(models) == 0 {
		return nil, errors.New("llm: Codex returned no selectable models")
	}
	return models, nil
}

func readRPCResult(dec *json.Decoder, wantID int) (json.RawMessage, error) {
	for {
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := dec.Decode(&msg); err != nil {
			return nil, err
		}
		var id int
		if len(msg.ID) == 0 || json.Unmarshal(msg.ID, &id) != nil || id != wantID {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("RPC %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (c *AgentClient) binaryPath() (string, error) {
	name := strings.TrimSpace(c.cfg.Binary)
	if name == "" {
		name = string(c.cfg.Provider)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s CLI is not installed", ErrNotConfigured, c.cfg.Provider)
	}
	return path, nil
}

func (c *AgentClient) env(workDir string) []string {
	processHome := c.cfg.ProcessHome
	if processHome == "" {
		processHome = c.cfg.HomeDir
	}
	env := []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=" + processHome,
		"TMPDIR=" + workDir,
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"DISABLE_AUTOUPDATER=1",
	}
	if _, err := os.Stat("/etc/ssl/certs/ca-certificates.crt"); err == nil {
		env = append(env, "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt")
	}
	if c.cfg.UseSystemAuth {
		// macOS Keychain lookup uses the login identity as well as HOME. These
		// are intentionally the only parent environment values inherited in
		// dev mode; Heya/database/provider secrets remain absent.
		for _, name := range []string{"USER", "LOGNAME", "SHELL"} {
			if value := os.Getenv(name); value != "" {
				env = append(env, name+"="+value)
			}
		}
	}
	switch c.cfg.Provider {
	case AgentClaude:
		env = append(env, "CLAUDE_CODE_SAFE_MODE=1")
		configDir := c.cfg.ConfigDir
		if configDir == "" && !c.cfg.UseSystemAuth {
			configDir = c.cfg.HomeDir
		}
		if configDir != "" {
			env = append(env, "CLAUDE_CONFIG_DIR="+configDir)
		}
		if c.cfg.OAuthToken != "" {
			env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+c.cfg.OAuthToken)
		}
	case AgentCodex:
		env = append(env, "CODEX_HOME="+c.cfg.HomeDir)
	}
	return env
}

func agentWorkDir(provider string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "heya-ai-"+provider+"-")
	if err != nil {
		return "", nil, fmt.Errorf("llm: create isolated work directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // directories need execute permission to be traversable
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func agentPrompt(messages []Message, maxTokens int) (string, string, error) {
	if len(messages) == 0 {
		return "", "", errors.New("llm: empty agent conversation")
	}
	systemParts := []string{
		"You are the inference engine inside Heya, a media server. Answer the request directly.",
		"Do not inspect files, run commands, edit code, browse, or invoke tools. Heya has supplied all relevant context.",
	}
	history := make([]Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) != "" {
				systemParts = append(systemParts, message.Content)
			}
		case "user", "assistant":
			history = append(history, message)
		default:
			return "", "", fmt.Errorf("llm: unsupported agent message role %q", message.Role)
		}
	}
	if len(history) == 0 {
		return "", "", errors.New("llm: agent conversation has no user message")
	}
	raw, err := json.Marshal(history)
	if err != nil {
		return "", "", err
	}
	prompt := "Continue this conversation with the next assistant response. Conversation JSON:\n" + string(raw)
	if maxTokens > 0 {
		prompt += fmt.Sprintf("\nKeep the response within approximately %d tokens.", maxTokens)
	}
	return strings.Join(systemParts, "\n\n"), prompt, nil
}

func runAgentCommand(
	ctx context.Context,
	bin string,
	args []string,
	stdin string,
	workDir string,
	env []string,
) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // resolved official binary, no shell
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func commandError(name string, err error, stderr []byte) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("llm: %s timed out", name)
	}
	message := safeOutput(string(stderr))
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("llm: %s failed: %s", name, message)
}

func safeOutput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4096 {
		s = s[:4096] + "…"
	}
	return s
}
