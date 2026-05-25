package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `heya api` — thin authenticated HTTP client for the local Heya server.
// Saves the bearer token to ~/.config/heya/cli-token after the first
// login so subsequent invocations are one round-trip; re-logs in on 401.
//
// Targets the common debugging shape: "I just want to hit /api/foo and
// see the JSON". Pretty-prints JSON by default, --raw bypasses for binary
// endpoints / piping to jq.

var (
	apiBaseURL string
	apiUser    string
	apiPass    string
	apiToken   string
	apiQuery   []string
	apiRaw     bool
)

var apiCmd = &cobra.Command{
	Use:   "api <method> <path> [body]",
	Short: "Issue an authenticated request to the local Heya API",
	Long: `Issues an HTTP request to the running Heya server.

The first call logs in (default admin/admin; override with --user / --pass
or HEYA_API_USER / HEYA_API_PASS), caches the bearer token under the OS
user config dir (heya/cli-token — on macOS that's
~/Library/Application Support/heya, on Linux $XDG_CONFIG_HOME or
~/.config/heya), and reuses it next time. A 401 triggers an automatic
re-login + one retry.

Body sources (positional, optional):
  '{"name":"alice"}'   literal JSON string
  @body.json           read from file
  -                    read from stdin

Query params: -q key=value, repeatable. Auto-URL-encoded.

Examples:
  heya api get /api/health
  heya api get /api/media -q type=music -q limit=5
  heya api post /api/users '{"username":"bob","email":"b@x","password":"hunter22"}'
  cat patch.json | heya api patch /api/media/42 -

Non-2xx responses print status + body to stderr and exit non-zero.`,
	Args:          cobra.RangeArgs(2, 3),
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE:          runAPI,
}

func init() {
	apiCmd.Flags().StringVar(&apiBaseURL, "base", envOrDefault("HEYA_API_BASE_URL", "http://localhost:8080"), "Server base URL")
	apiCmd.Flags().StringVar(&apiUser, "user", envOrDefault("HEYA_API_USER", "admin"), "Login username")
	apiCmd.Flags().StringVar(&apiPass, "pass", envOrDefault("HEYA_API_PASS", "admin"), "Login password")
	apiCmd.Flags().StringVar(&apiToken, "token", os.Getenv("HEYA_API_TOKEN"), "Bearer token (skips login + cache)")
	apiCmd.Flags().StringSliceVarP(&apiQuery, "query", "q", nil, "Query param key=value (repeatable)")
	apiCmd.Flags().BoolVar(&apiRaw, "raw", false, "Stream response bytes verbatim (no JSON pretty-print)")
	rootCmd.AddCommand(apiCmd)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runAPI(cmd *cobra.Command, args []string) error {
	method := strings.ToUpper(args[0])
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions:
	default:
		return fmt.Errorf("unsupported method %q (want get/post/put/patch/delete/head/options)", strings.ToLower(method))
	}

	path := args[1]
	fullURL, err := buildAPIURL(apiBaseURL, path, apiQuery)
	if err != nil {
		return err
	}

	var body []byte
	if len(args) == 3 {
		body, err = readAPIBody(args[2])
		if err != nil {
			return err
		}
	}

	ctx := cmd.Context()

	token, err := obtainAPIToken(ctx)
	if err != nil {
		return err
	}

	resp, err := doAPIRequest(ctx, method, fullURL, token, body)
	if err != nil {
		return err
	}

	// Re-auth on 401, but only when the token came from cache/auto-login.
	// An explicit --token / HEYA_API_TOKEN means the caller pinned the
	// value on purpose; refreshing it would surprise scripts.
	if resp.StatusCode == http.StatusUnauthorized && apiToken == "" {
		_ = resp.Body.Close()
		_ = clearAPITokenCache()
		token, err = loginAndCacheAPI(ctx)
		if err != nil {
			return fmt.Errorf("re-login failed: %w", err)
		}
		resp, err = doAPIRequest(ctx, method, fullURL, token, body)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close() //nolint:errcheck // defer-close on response body

	return writeAPIResponse(resp)
}

// buildAPIURL joins the base + path and appends query params.
func buildAPIURL(base, path string, kvs []string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	q := u.Query()
	for _, kv := range kvs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return "", fmt.Errorf("bad --query %q (expected key=value)", kv)
		}
		q.Add(parts[0], parts[1])
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// readAPIBody resolves the positional body argument:
//   - "-"        → read all of stdin
//   - "@path"    → read the file
//   - otherwise  → literal bytes (typically JSON)
func readAPIBody(arg string) ([]byte, error) {
	switch {
	case arg == "":
		return nil, nil
	case arg == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(arg, "@"):
		path := arg[1:]
		data, err := os.ReadFile(path) //nolint:gosec // CLI tool reading user-supplied path is intended.
		if err != nil {
			return nil, fmt.Errorf("read body file %s: %w", path, err)
		}
		return data, nil
	default:
		return []byte(arg), nil
	}
}

// obtainAPIToken picks a token in priority order: --token / env > on-disk
// cache > fresh login.
func obtainAPIToken(ctx context.Context) (string, error) {
	if apiToken != "" {
		return apiToken, nil
	}
	if tok, ok := readAPITokenCache(); ok {
		return tok, nil
	}
	return loginAndCacheAPI(ctx)
}

func apiTokenCachePath() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "heya", "cli-token"), nil
}

func readAPITokenCache() (string, bool) {
	path, err := apiTokenCachePath()
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path) //nolint:gosec // Cache file under the user's own config dir.
	if err != nil {
		return "", false
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", false
	}
	return tok, true
}

func writeAPITokenCache(tok string) error {
	path, err := apiTokenCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tok+"\n"), 0o600)
}

func clearAPITokenCache() error {
	path, err := apiTokenCachePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func loginAndCacheAPI(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": apiUser, "password": apiPass})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(apiBaseURL, "/")+"/api/auth/login",
		bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer-close on response body
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("login as %q failed: HTTP %d: %s", apiUser, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("login: decode response: %w", err)
	}
	if out.Token == "" {
		return "", errors.New("login succeeded but response carried no token")
	}
	// Cache write is best-effort — a read-only home directory still lets
	// the request through, the next invocation just re-logs in.
	_ = writeAPITokenCache(out.Token)
	return out.Token, nil
}

func doAPIRequest(ctx context.Context, method, fullURL, token string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if len(body) > 0 {
		// Default to JSON; the user can override via stdin pipe + a body
		// type if they ever need form encoding, but every internal
		// endpoint takes JSON.
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	// Long ceiling so heavy refresh endpoints (heya.media artist enrich
	// can run 60-120s cold) don't time out from the CLI. The server has
	// its own request timeouts; this is just a safety net.
	client := &http.Client{Timeout: 5 * time.Minute}
	return client.Do(req)
}

// writeAPIResponse renders the response body. 2xx → stdout, non-2xx →
// stderr (with a leading status line) and exit code 1.
//
// JSON bodies are pretty-printed unless --raw is set or the response
// doesn't declare a JSON content-type. Binary streams (images,
// audio/video) print to stdout verbatim when --raw is on, so callers
// can pipe `heya api get /api/tracks/123/stream --raw > out.flac`.
func writeAPIResponse(resp *http.Response) error {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	target := os.Stdout
	if !ok {
		target = os.Stderr
		fmt.Fprintf(target, "HTTP %d %s\n", resp.StatusCode, resp.Status) //nolint:errcheck // diagnostic stderr write
	}

	pretty := !apiRaw && isAPIJSONContent(resp)
	if pretty {
		var v interface{}
		if jsonErr := json.Unmarshal(raw, &v); jsonErr == nil {
			out, _ := json.MarshalIndent(v, "", "  ")
			_, _ = target.Write(out)
			_, _ = target.Write([]byte("\n"))
		} else {
			// Content-Type lied — fall back to raw passthrough.
			_, _ = target.Write(raw)
			ensureTrailingNewline(target, raw)
		}
	} else {
		_, _ = target.Write(raw)
		ensureTrailingNewline(target, raw)
	}

	if !ok {
		// Cobra's default error path also exits non-zero, but it prints a
		// trailing "Error: ..." line. We've already printed the status +
		// body to stderr — just exit cleanly.
		os.Exit(1)
	}
	return nil
}

func ensureTrailingNewline(w io.Writer, raw []byte) {
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		_, _ = w.Write([]byte("\n"))
	}
}

func isAPIJSONContent(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/problem+json") ||
		strings.Contains(ct, "+json")
}
