package opensubtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const baseURL = "https://api.opensubtitles.com/api/v1"

type Client struct {
	apiKey     string
	httpClient *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
	username  string
	password  string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) SetCredentials(username, password string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.username = username
	c.password = password
	c.token = ""
	c.expiresAt = time.Time{}
}

func (c *Client) Login(ctx context.Context) error {
	// G117: LoginRequest legitimately marshals the OpenSubtitles credential
	// payload — this is the documented sign-in body, not an accidental leak.
	body, _ := json.Marshal(LoginRequest{ //nolint:gosec
		Username: c.username,
		Password: c.password,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed (status %d): %s", resp.StatusCode, string(b))
	}

	var result LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("login decode failed: %w", err)
	}

	c.mu.Lock()
	c.token = result.Token
	c.expiresAt = time.Now().Add(23 * time.Hour)
	c.mu.Unlock()

	return nil
}

func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	hasToken := c.token != "" && time.Now().Before(c.expiresAt)
	c.mu.Unlock()

	if hasToken {
		return nil
	}
	return c.Login(ctx)
}

func (c *Client) doGet(ctx context.Context, path string) (*http.Response, error) {
	return c.doGetRetry(ctx, path, true)
}

func (c *Client) doGetRetry(ctx context.Context, path string, retryUnauthorized bool) (*http.Response, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	req.Header.Set("Authorization", "Bearer "+c.token)
	c.mu.Unlock()
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized && retryUnauthorized {
		_ = resp.Body.Close()
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		return c.doGetRetry(ctx, path, false)
	}

	return resp, nil
}

func (c *Client) doPost(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.doPostRetry(ctx, path, body, true)
}

func (c *Client) doPostRetry(ctx context.Context, path string, body any, retryUnauthorized bool) (*http.Response, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	req.Header.Set("Authorization", "Bearer "+c.token)
	c.mu.Unlock()
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized && retryUnauthorized {
		_ = resp.Body.Close()
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		return c.doPostRetry(ctx, path, body, false)
	}

	return resp, nil
}
