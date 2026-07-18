// Package acoustid performs read-only Chromaprint lookups against AcoustID.
// Fingerprint submission is deliberately out of scope: lookup needs only the
// server's application key and cannot publish a user's library metadata.
package acoustid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://api.acoustid.org"

var ErrDisabled = errors.New("acoustid lookup is disabled")

type Match struct {
	AcoustID      string  `json:"acoustid"`
	RecordingMBID string  `json:"recording_mbid"`
	Score         float64 `json:"score"`
}

type Options struct {
	BaseURL           string
	APIKey            string
	RequestsPerSecond int
	HTTPClient        *http.Client
}

type Client struct {
	baseURL *url.URL
	apiKey  string
	http    *http.Client
	limiter *rate.Limiter
}

func New(options Options) (*Client, error) {
	base := strings.TrimSpace(options.BaseURL)
	if base == "" {
		base = defaultBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("acoustid base URL %q is invalid", base)
	}
	requestsPerSecond := options.RequestsPerSecond
	if requestsPerSecond <= 0 || requestsPerSecond > 3 {
		requestsPerSecond = 3
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL: parsed,
		apiKey:  strings.TrimSpace(options.APIKey),
		http:    httpClient,
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1),
	}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.apiKey != "" }

func (c *Client) Lookup(ctx context.Context, fingerprint string, durationSecs int) ([]Match, error) {
	if !c.Enabled() {
		return nil, ErrDisabled
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" || durationSecs <= 0 {
		return nil, errors.New("acoustid lookup requires fingerprint and source duration")
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/v2/lookup"
	form := url.Values{
		"client":      {c.apiKey},
		"duration":    {strconv.Itoa(durationSecs)},
		"fingerprint": {fingerprint},
		"format":      {"json"},
		"meta":        {"recordingids"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Heya music matcher")

	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acoustid lookup: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read acoustid response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acoustid lookup: HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Status string `json:"status"`
		Error  struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Results []struct {
			ID         string  `json:"id"`
			Score      float64 `json:"score"`
			Recordings []struct {
				ID string `json:"id"`
			} `json:"recordings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode acoustid response: %w", err)
	}
	if envelope.Status != "ok" {
		return nil, fmt.Errorf("acoustid lookup failed (%d): %s", envelope.Error.Code, envelope.Error.Message)
	}

	byRecording := map[string]Match{}
	for _, result := range envelope.Results {
		for _, recording := range result.Recordings {
			mbid := strings.ToLower(strings.TrimSpace(recording.ID))
			if mbid == "" {
				continue
			}
			candidate := Match{AcoustID: result.ID, RecordingMBID: mbid, Score: result.Score}
			if previous, exists := byRecording[mbid]; !exists || candidate.Score > previous.Score {
				byRecording[mbid] = candidate
			}
		}
	}
	matches := make([]Match, 0, len(byRecording))
	for _, match := range byRecording {
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].RecordingMBID < matches[j].RecordingMBID
	})
	return matches, nil
}
