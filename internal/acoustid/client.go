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

type ErrorClass string

const (
	ErrorPermanent     ErrorClass = "permanent"
	ErrorTransient     ErrorClass = "transient"
	ErrorConfiguration ErrorClass = "configuration"
)

// LookupError classifies an AcoustID failure so durable scanner callers can
// distinguish a service/network outage from a bad application key or invalid
// fingerprint. RetryAfter is populated when the server supplies one.
type LookupError struct {
	Class      ErrorClass
	Message    string
	StatusCode int
	RetryAfter time.Duration
	Underlying error
}

func (e *LookupError) Error() string {
	if e == nil {
		return "acoustid lookup failed"
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Underlying != nil {
		return e.Underlying.Error()
	}
	return "acoustid lookup failed"
}

func (e *LookupError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Underlying
}

// IsConfigurationError lets higher-level scanner code surface a global bad
// key/base configuration as a job error without importing this package.
func (e *LookupError) IsConfigurationError() bool {
	return e != nil && e.Class == ErrorConfiguration
}

func IsTransient(err error) bool {
	var lookupErr *LookupError
	return errors.As(err, &lookupErr) && lookupErr.Class == ErrorTransient
}

func IsConfiguration(err error) bool {
	if errors.Is(err, ErrDisabled) {
		return true
	}
	var lookupErr *LookupError
	return errors.As(err, &lookupErr) && lookupErr.Class == ErrorConfiguration
}

func ErrorRetryAfter(err error) time.Duration {
	var lookupErr *LookupError
	if errors.As(err, &lookupErr) {
		return lookupErr.RetryAfter
	}
	return 0
}

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
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &LookupError{Class: ErrorTransient, Message: "acoustid lookup: " + err.Error(), Underlying: err}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, &LookupError{Class: ErrorTransient, Message: "read acoustid response: " + err.Error(), Underlying: err}
	}
	if response.StatusCode != http.StatusOK {
		var errorEnvelope struct {
			Status string `json:"status"`
			Error  struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &errorEnvelope) == nil && errorEnvelope.Status != "ok" &&
			(errorEnvelope.Error.Code != 0 || strings.TrimSpace(errorEnvelope.Error.Message) != "") {
			return nil, &LookupError{
				Class:      acoustIDEnvelopeErrorClass(errorEnvelope.Error.Code, errorEnvelope.Error.Message),
				StatusCode: response.StatusCode,
				RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
				Message:    fmt.Sprintf("acoustid lookup failed (%d): %s", errorEnvelope.Error.Code, errorEnvelope.Error.Message),
			}
		}
		class := ErrorPermanent
		switch {
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
			class = ErrorConfiguration
		case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
			class = ErrorTransient
		}
		return nil, &LookupError{
			Class: class, StatusCode: response.StatusCode,
			RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
			Message:    fmt.Sprintf("acoustid lookup: HTTP %d", response.StatusCode),
		}
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
		return nil, &LookupError{Class: ErrorTransient, Message: "decode acoustid response: " + err.Error(), Underlying: err}
	}
	if envelope.Status != "ok" {
		class := acoustIDEnvelopeErrorClass(envelope.Error.Code, envelope.Error.Message)
		return nil, &LookupError{
			Class:   class,
			Message: fmt.Sprintf("acoustid lookup failed (%d): %s", envelope.Error.Code, envelope.Error.Message),
		}
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

func acoustIDEnvelopeErrorClass(code int, message string) ErrorClass {
	// AcoustID code 4 is an invalid client/application key; codes 5 and 9 are
	// invalid user credentials used by submission endpoints. Treat all three
	// as configuration so they never masquerade as an ordinary no-match.
	switch code {
	case 4, 5, 9:
		return ErrorConfiguration
	case 10:
		return ErrorTransient
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "client key") || strings.Contains(lower, "api key") || strings.Contains(lower, "authentication") {
		return ErrorConfiguration
	}
	for _, fragment := range []string{"internal error", "temporarily", "rate limit", "too many", "unavailable", "timeout"} {
		if strings.Contains(lower, fragment) {
			return ErrorTransient
		}
	}
	return ErrorPermanent
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}
