// Package sabnzbd speaks SABnzbd's JSON API. Phase 1 needs version + queue
// (queue is the authenticated call that actually validates the API key —
// mode=version answers without auth).
package sabnzbd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.call(ctx, url.Values{"mode": {"version"}}, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

type Queue struct {
	Paused          bool        `json:"paused"`
	SpeedKBps       string      `json:"kbpersec"`
	SizeLeftMB      string      `json:"mbleft"`
	DiskspaceFreeGB string      `json:"diskspace1"`
	Slots           []QueueSlot `json:"slots"`
}

type QueueSlot struct {
	NzoID      string `json:"nzo_id"`
	Filename   string `json:"filename"`
	Category   string `json:"cat"`
	Status     string `json:"status"`
	SizeMB     string `json:"mb"`
	SizeLeftMB string `json:"mbleft"`
	Percentage string `json:"percentage"`
	TimeLeft   string `json:"timeleft"`
}

// Queue returns the live download queue. This is also the connection test:
// it fails with "API Key Incorrect" on a bad key where mode=version wouldn't.
func (c *Client) Queue(ctx context.Context) (*Queue, error) {
	var out struct {
		Queue *Queue `json:"queue"`
	}
	if err := c.call(ctx, url.Values{"mode": {"queue"}, "limit": {"50"}}, &out); err != nil {
		return nil, err
	}
	if out.Queue == nil {
		return nil, fmt.Errorf("sabnzbd: queue response missing queue object")
	}
	return out.Queue, nil
}

// ServerStats is the lifetime/day/week/month downloaded byte counters.
type ServerStats struct {
	Total int64 `json:"total"`
	Month int64 `json:"month"`
	Week  int64 `json:"week"`
	Day   int64 `json:"day"`
}

func (c *Client) ServerStats(ctx context.Context) (*ServerStats, error) {
	var out ServerStats
	if err := c.call(ctx, url.Values{"mode": {"server_stats"}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type HistorySlot struct {
	NzoID       string `json:"nzo_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Category    string `json:"category"`
	Bytes       int64  `json:"bytes"`
	Storage     string `json:"storage"`
	FailMessage string `json:"fail_message"`
	// Completed is a unix timestamp.
	Completed int64 `json:"completed"`
}

func (c *Client) History(ctx context.Context, limit int) ([]HistorySlot, error) {
	var out struct {
		History struct {
			Slots []HistorySlot `json:"slots"`
		} `json:"history"`
	}
	params := url.Values{"mode": {"history"}}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if err := c.call(ctx, params, &out); err != nil {
		return nil, err
	}
	return out.History.Slots, nil
}

// ConfigCategory is one SAB category; Dir is relative to CompleteDir unless
// absolute.
type ConfigCategory struct {
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Priority int    `json:"priority"`
	Script   string `json:"script"`
}

type Config struct {
	CompleteDir string
	Categories  []ConfigCategory
}

func (c *Client) Config(ctx context.Context) (*Config, error) {
	var out struct {
		Config struct {
			Misc struct {
				CompleteDir string `json:"complete_dir"`
			} `json:"misc"`
			Categories []ConfigCategory `json:"categories"`
		} `json:"config"`
	}
	if err := c.call(ctx, url.Values{"mode": {"get_config"}}, &out); err != nil {
		return nil, err
	}
	return &Config{
		CompleteDir: out.Config.Misc.CompleteDir,
		Categories:  out.Config.Categories,
	}, nil
}

type Warning struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Time int64  `json:"time"`
}

func (c *Client) Warnings(ctx context.Context) ([]Warning, error) {
	var out struct {
		Warnings []Warning `json:"warnings"`
	}
	if err := c.call(ctx, url.Values{"mode": {"warnings"}}, &out); err != nil {
		return nil, err
	}
	return out.Warnings, nil
}

func (c *Client) call(ctx context.Context, params url.Values, out any) error {
	params.Set("output", "json")
	params.Set("apikey", c.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("sabnzbd: building request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("sabnzbd: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer close
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("sabnzbd: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sabnzbd: returned HTTP %d", resp.StatusCode)
	}
	// SAB reports some errors as 200s with {"status": false, "error": "…"},
	// and auth failures as bare plain text ("API Key Incorrect").
	var apiErr struct {
		Status *bool  `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" && apiErr.Status != nil && !*apiErr.Status {
		return fmt.Errorf("sabnzbd: %s", apiErr.Error)
	}
	if err := json.Unmarshal(body, out); err != nil {
		if text := strings.TrimSpace(string(body)); text != "" && len(text) < 200 && !strings.HasPrefix(text, "{") {
			return fmt.Errorf("sabnzbd: %s", text)
		}
		return fmt.Errorf("sabnzbd: parsing response: %w", err)
	}
	return nil
}
