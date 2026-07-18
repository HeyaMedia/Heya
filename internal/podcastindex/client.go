// Package podcastindex is a thin HTTP client for the api.podcastindex.org
// developer API. Provides trending / search / categories along with helpers
// for parsing arbitrary RSS feeds (the podcast detail page hits the feed
// URL directly so episode listings aren't capped by PI's free-tier limits).
//
// PI's auth model is a per-request HMAC: SHA1(key + secret + unix_ts). The
// client signs every outbound request transparently.
package podcastindex

import (
	"context"
	"crypto/sha1" //nolint:gosec // upstream API specifies SHA1
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/httpbodylimit"
)

const (
	piBase    = "https://api.podcastindex.org/api/1.0"
	userAgent = "Heya/0.1 (+https://heya.media)"
	cacheTTL  = 15 * time.Minute
	// Search/trending/category payloads are JSON and should remain far below
	// this ceiling even for large result pages.
	maxAPIResponseBytes int64 = 16 << 20
)

// ErrUnconfigured fires when the caller hits a method that needs PI auth
// without the credentials being set. The HTTP handler turns this into a
// clear 503 so the FE can show a "set the key in Settings" message.
var ErrUnconfigured = errors.New("podcast-index API key not configured")

// Podcast is the projection of a /search or /trending feed entry the FE
// renders as a card. Snake-case to match the hibiki podcast layer.
type Podcast struct {
	ID           int64             `json:"id"`
	Title        string            `json:"title"`
	Author       string            `json:"author"`
	Description  string            `json:"description"`
	ArtworkURL   string            `json:"artwork_url"`
	FeedURL      string            `json:"feed_url"`
	Categories   map[string]string `json:"categories"`
	Language     string            `json:"language"`
	EpisodeCount int               `json:"episode_count"`
}

// Category is one entry in /categories/list.
type Category struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	http   *http.Client
	key    string
	secret string

	mu   sync.Mutex
	data map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	expires time.Time
}

// New builds a client. Both empty strings = "unconfigured" — methods will
// return ErrUnconfigured rather than hitting the API with bad creds.
func New(key, secret string) *Client {
	return &Client{
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: httpbodylimit.NewTransport(nil, maxAPIResponseBytes),
		},
		key:    key,
		secret: secret,
		data:   make(map[string]cacheEntry),
	}
}

// Configured reports whether the client has credentials wired up — handlers
// check this so they can fast-fail with a clean 503.
func (c *Client) Configured() bool {
	return c.key != "" && c.secret != ""
}

// authHeaders signs one request. The signature is SHA1(key + secret + ts).
// PI updates the spec rarely; if they ever switch hashes, change here.
func (c *Client) authHeaders() http.Header {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sum := sha1.Sum([]byte(c.key + c.secret + ts)) //nolint:gosec // upstream-mandated SHA1
	h := http.Header{}
	h.Set("User-Agent", userAgent)
	h.Set("X-Auth-Key", c.key)
	h.Set("X-Auth-Date", ts)
	h.Set("Authorization", hex.EncodeToString(sum[:]))
	return h
}

func (c *Client) cachedGet(ctx context.Context, cacheKey string, ttl time.Duration, build func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if entry, ok := c.data[cacheKey]; ok && time.Now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.body, nil
	}
	c.mu.Unlock()

	body, err := build()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.data[cacheKey] = cacheEntry{body: body, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
	_ = ctx // reserved for tracing; suppresses unused-arg lint without changing the signature.
	return body, nil
}

func (c *Client) apiGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if !c.Configured() {
		return nil, ErrUnconfigured
	}
	u, err := url.Parse(piBase + path)
	if err != nil {
		return nil, err
	}
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range c.authHeaders() {
		req.Header[k] = v
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("podcast-index fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer close
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("podcast-index %s: HTTP %d", path, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// rawFeed mirrors the upstream JSON shape so we can rebuild Podcast cleanly.
type rawFeed struct {
	ID           int64             `json:"id"`
	Title        string            `json:"title"`
	Author       string            `json:"author"`
	Description  string            `json:"description"`
	Artwork      string            `json:"artwork"`
	URL          string            `json:"url"`
	Categories   map[string]string `json:"categories"`
	Language     string            `json:"language"`
	EpisodeCount int               `json:"episodeCount"`
}

func (r rawFeed) toPodcast() Podcast {
	cats := r.Categories
	if cats == nil {
		cats = map[string]string{}
	}
	return Podcast{
		ID:           r.ID,
		Title:        r.Title,
		Author:       r.Author,
		Description:  r.Description,
		ArtworkURL:   r.Artwork,
		FeedURL:      r.URL,
		Categories:   cats,
		Language:     r.Language,
		EpisodeCount: r.EpisodeCount,
	}
}

// Search runs /search/byterm. Empty query → empty result (no upstream call).
func (c *Client) Search(ctx context.Context, query string, max int) ([]Podcast, error) {
	if query == "" {
		return []Podcast{}, nil
	}
	if max <= 0 || max > 100 {
		max = 20
	}
	cacheKey := fmt.Sprintf("search:%s:%d", query, max)
	body, err := c.cachedGet(ctx, cacheKey, cacheTTL, func() ([]byte, error) {
		q := url.Values{}
		q.Set("q", query)
		q.Set("max", fmt.Sprintf("%d", max))
		return c.apiGet(ctx, "/search/byterm", q)
	})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Feeds []rawFeed `json:"feeds"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	out := make([]Podcast, 0, len(wrap.Feeds))
	for _, f := range wrap.Feeds {
		out = append(out, f.toPodcast())
	}
	return out, nil
}

// Trending returns /podcasts/trending, optionally filtered to one category.
// Categories take the comma-separated name list PI uses (e.g. "Music,Arts").
func (c *Client) Trending(ctx context.Context, max int, category string) ([]Podcast, error) {
	if max <= 0 || max > 100 {
		max = 15
	}
	cacheKey := fmt.Sprintf("trending:%d:%s", max, category)
	body, err := c.cachedGet(ctx, cacheKey, 1*time.Hour, func() ([]byte, error) {
		q := url.Values{}
		q.Set("max", fmt.Sprintf("%d", max))
		q.Set("lang", "en")
		if category != "" {
			q.Set("cat", category)
		}
		return c.apiGet(ctx, "/podcasts/trending", q)
	})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Feeds []rawFeed `json:"feeds"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("decode trending: %w", err)
	}
	out := make([]Podcast, 0, len(wrap.Feeds))
	for _, f := range wrap.Feeds {
		out = append(out, f.toPodcast())
	}
	return out, nil
}

// Categories returns /categories/list. Cached aggressively — the list
// changes rarely.
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	body, err := c.cachedGet(ctx, "categories", 24*time.Hour, func() ([]byte, error) {
		return c.apiGet(ctx, "/categories/list", url.Values{})
	})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Feeds []Category `json:"feeds"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	return wrap.Feeds, nil
}
