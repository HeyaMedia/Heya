package dashboard

import (
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
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) GetToken() string { return c.token }

func (c *Client) Login(ctx context.Context, username, password string) error {
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/auth/login", strReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("login failed (HTTP %d)", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
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
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func strReader(s string) io.Reader {
	return io.NopCloser(readString(s))
}

type stringReader struct {
	s string
	i int
}

func readString(s string) *stringReader { return &stringReader{s: s} }

func (r *stringReader) Read(p []byte) (n int, err error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n = copy(p, r.s[r.i:])
	r.i += n
	return
}
