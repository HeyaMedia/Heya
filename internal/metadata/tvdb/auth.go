package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type tokenManager struct {
	mu        sync.Mutex
	apiKey    string
	token     string
	expiresAt time.Time
	baseURL   string
}

type loginRequest struct {
	APIKey string `json:"apikey"`
}

type loginResponse struct {
	Status string `json:"status"`
	Data   struct {
		Token string `json:"token"`
	} `json:"data"`
}

func newTokenManager(apiKey, baseURL string) *tokenManager {
	return &tokenManager{apiKey: apiKey, baseURL: baseURL}
}

func (tm *tokenManager) getToken(ctx context.Context) (string, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.token != "" && time.Now().Before(tm.expiresAt) {
		return tm.token, nil
	}

	body, _ := json.Marshal(loginRequest{APIKey: tm.apiKey})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tm.baseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("tvdb login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tvdb login: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tvdb login: HTTP %d", resp.StatusCode)
	}

	var lr loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return "", fmt.Errorf("tvdb login decode: %w", err)
	}

	tm.token = lr.Data.Token
	tm.expiresAt = time.Now().Add(27 * 24 * time.Hour)
	return tm.token, nil
}
