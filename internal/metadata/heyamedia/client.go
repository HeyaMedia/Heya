package heyamedia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) get(ctx context.Context, path string, params url.Values, result any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("heyamedia %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("heyamedia %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func (c *Client) getJSON(ctx context.Context, path string, result any) error {
	return c.get(ctx, path, nil, result)
}

type imageProxyResponse struct {
	PublicURL string `json:"public_url"`
	FromCache bool   `json:"from_cache"`
}

// ProxyImageURL sends an upstream image URL through HeyaMedia's image proxy,
// which caches it to B2 and returns a CDN URL.
func (c *Client) ProxyImageURL(ctx context.Context, upstreamURL string) string {
	if upstreamURL == "" {
		return ""
	}
	var resp imageProxyResponse
	params := url.Values{"url": {upstreamURL}}
	if err := c.get(ctx, "/api/v1/image", params, &resp); err != nil {
		return upstreamURL
	}
	return resp.PublicURL
}
