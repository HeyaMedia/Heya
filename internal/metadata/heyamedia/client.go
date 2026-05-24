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
			// Ceiling for a single HeyaMedia HTTP call. Search is fast
			// (sub-second to a few seconds) but artist GetDetail can
			// legitimately take up to 120s on cold cache because
			// HeyaMedia is rate-limited by its upstream music providers
			// — give it room. Callers can cancel sooner via ctx; this
			// is just the worst-case backstop so a hung HeyaMedia
			// doesn't wedge a worker forever.
			Timeout: 5 * time.Minute,
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
