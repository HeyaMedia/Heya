package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return NewClientWithHTTP(baseURL, token, &http.Client{Timeout: 5 * time.Second})
}

func NewClientWithHTTP(baseURL, token string, client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    client,
	}
}

func (c *Client) GetToken() string { return c.token }

func (c *Client) Login(ctx context.Context, username, password string) error {
	// The password is intentionally serialized into the authenticated login request body.
	//nolint:gosec
	body, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{Username: username, Password: password})
	if err != nil {
		return fmt.Errorf("encode login request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed (HTTP %d)", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	if result.Token == "" {
		return fmt.Errorf("login response did not include a token")
	}
	c.token = result.Token
	return nil
}

type OverviewData struct {
	Libraries  []LibraryData `json:"libraries"`
	MediaCount map[string]int64
	JobSummary map[string]int64
	Watchers   []WatcherEntry
}

type LibraryData struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	MediaType string   `json:"media_type"`
	Paths     []string `json:"paths"`
}

type WatcherEntry struct {
	LibraryID int64  `json:"library_id"`
	Path      string `json:"path"`
}

func (c *Client) FetchLibraries(ctx context.Context) ([]LibraryData, error) {
	var libs []LibraryData
	err := c.getJSON(ctx, "/api/libraries", &libs)
	return libs, err
}

func (c *Client) FetchWatchers(ctx context.Context) ([]WatcherEntry, error) {
	var result struct {
		Watchers []WatcherEntry `json:"watchers"`
		Count    int            `json:"count"`
	}
	err := c.getJSON(ctx, "/api/watchers", &result)
	return result.Watchers, err
}

func (c *Client) FetchMediaCount(ctx context.Context, mediaType string) (int64, error) {
	var items []json.RawMessage
	err := c.getJSON(ctx, "/api/media?type="+mediaType+"&limit=0&offset=0", &items)
	return int64(len(items)), err
}

func (c *Client) FetchFileStats(ctx context.Context, libraryID int64) (map[string]int64, error) {
	var stats []struct {
		Status string `json:"status"`
		Count  int64  `json:"count"`
	}
	err := c.getJSON(ctx, fmt.Sprintf("/api/libraries/%d/files/stats", libraryID), &stats)
	if err != nil {
		return nil, err
	}
	m := make(map[string]int64)
	for _, s := range stats {
		m[s.Status] = s.Count
	}
	return m, nil
}

func (c *Client) getJSON(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
