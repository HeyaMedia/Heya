package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

type RateLimitedClient struct {
	client  *http.Client
	limiter *rate.Limiter
	ua      string
}

func NewRateLimitedClient(rps float64, burst int, userAgent string) *RateLimitedClient {
	return &RateLimitedClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		limiter: rate.NewLimiter(rate.Limit(rps), burst),
		ua:      userAgent,
	}
}

func (c *RateLimitedClient) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	if c.ua != "" {
		req.Header.Set("User-Agent", c.ua)
	}

	var resp *http.Response
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = c.client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		break
	}

	return resp, err
}

func (c *RateLimitedClient) GetJSON(ctx context.Context, url string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}
