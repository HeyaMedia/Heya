// Package arr is a minimal read-only client for Sonarr/Radarr/Lidarr, used
// to import custom formats and quality profiles from a running instance.
// Payloads come back raw; internal/manager/formats owns normalization.
package arr

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
	BaseURL    string
	APIKey     string
	apiVersion string
	HTTP       *http.Client
}

// New builds a client for the given app kind ('radarr', 'sonarr', 'lidarr').
// Lidarr still serves its API under /api/v1; the others moved to /api/v3.
func New(kind, baseURL, apiKey string) *Client {
	version := "v3"
	if kind == "lidarr" {
		version = "v1"
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		apiVersion: version,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/api/%s/%s", c.BaseURL, c.apiVersion, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // defer close
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("arr: invalid API key")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arr: %s returned %s", path, resp.Status)
	}
	return body, nil
}

// Version probes system/status and doubles as the connection test.
func (c *Client) Version(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "system/status")
	if err != nil {
		return "", err
	}
	var status struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return "", fmt.Errorf("arr: unexpected system/status payload: %w", err)
	}
	return status.Version, nil
}

func (c *Client) CustomFormatsRaw(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "customformat")
}

func (c *Client) QualityProfilesRaw(ctx context.Context) ([]byte, error) {
	return c.get(ctx, "qualityprofile")
}
