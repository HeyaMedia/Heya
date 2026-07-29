// Package prowlarr speaks Prowlarr's v1 app API — just enough to test the
// connection and sync its indexer list. Actual searching goes through each
// indexer's Torznab endpoint ({base}/{indexerId}/api), which Prowlarr exposes
// using the same app API key.
package prowlarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

type SystemStatus struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

type Indexer struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Enable       bool   `json:"enable"`
	Protocol     string `json:"protocol"` // "usenet" | "torrent"
	Priority     int    `json:"priority"`
	Privacy      string `json:"privacy"`
	Capabilities struct {
		Categories []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"categories"`
	} `json:"capabilities"`
}

func (c *Client) SystemStatus(ctx context.Context) (*SystemStatus, error) {
	var status SystemStatus
	if err := c.get(ctx, "/api/v1/system/status", &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *Client) Indexers(ctx context.Context) ([]Indexer, error) {
	var indexers []Indexer
	if err := c.get(ctx, "/api/v1/indexer", &indexers); err != nil {
		return nil, err
	}
	return indexers, nil
}

// TorznabURL is the per-indexer Torznab endpoint Prowlarr exposes for the
// given indexer id, authenticated with the same app API key.
func (c *Client) TorznabURL(indexerID int) string {
	return fmt.Sprintf("%s/%d/api", c.BaseURL, indexerID)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("prowlarr: building request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("prowlarr: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer close
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("prowlarr: reading response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("prowlarr: API key rejected")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prowlarr: %s returned HTTP %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("prowlarr: parsing %s response: %w", path, err)
	}
	return nil
}
