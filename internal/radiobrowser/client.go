// Package radiobrowser is a thin client + cache for the community-run
// radio-browser.info API. Mirrors the hibiki radio-browser layer's surface
// (search, top, countries, tags, click) but in Go so it can live alongside
// the rest of the Heya backend instead of as a separate Nuxt server route.
//
// Discovery: radio-browser publishes a list of mirror servers; we pick
// randomly to spread load. Both the server pool and per-query results are
// cached in memory with bounded TTLs.
package radiobrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Station is one entry in a radio-browser search/list response. Fields
// match the upstream JSON shape verbatim so the FE can pass them straight
// through to the favorites table.
type Station struct {
	StationUUID string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	URLResolved string `json:"url_resolved"`
	Favicon     string `json:"favicon"`
	Homepage    string `json:"homepage"`
	Country     string `json:"country"`
	CountryCode string `json:"countrycode"`
	Language    string `json:"language"`
	Tags        string `json:"tags"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	Votes       int    `json:"votes"`
	ClickCount  int    `json:"clickcount"`
}

// Country is one entry in /json/countries.
type Country struct {
	Name         string `json:"name"`
	Iso31661     string `json:"iso_3166_1"`
	StationCount int    `json:"stationcount"`
}

// Tag is one entry in /json/tags.
type Tag struct {
	Name         string `json:"name"`
	StationCount int    `json:"stationcount"`
}

// fallbackServers is the bootstrap list when DNS discovery fails. radio-
// browser publishes A/AAAA records at all.api.radio-browser.info that fan
// out to its mirrors — but a static fallback keeps cold starts working when
// the discovery host is unreachable.
var fallbackServers = []string{
	"https://de1.api.radio-browser.info",
	"https://nl1.api.radio-browser.info",
	"https://at1.api.radio-browser.info",
}

const (
	serverCacheTTL = 1 * time.Hour
	dataCacheTTL   = 5 * time.Minute
	userAgent      = "Heya/0.1 (+https://heya.media)"
)

// Client is concurrency-safe; instances are cheap so call sites typically
// just keep one in App or build per-request as needed.
type Client struct {
	http *http.Client

	mu             sync.Mutex
	servers        []string
	serversFetched time.Time
	data           map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	expires time.Time
}

// New returns a Client with sensible HTTP timeouts. The 15s outer timeout
// covers slow upstream mirrors; individual API responses are tiny so the
// real cost is connect + TLS handshake.
func New() *Client {
	return &Client{
		http: &http.Client{Timeout: 15 * time.Second},
		data: make(map[string]cacheEntry),
	}
}

// discoverServers refreshes the mirror list, falling back to the hardcoded
// pool on any error so the API stays reachable when discovery is down.
func (c *Client) discoverServers(ctx context.Context) []string {
	c.mu.Lock()
	if time.Since(c.serversFetched) < serverCacheTTL && len(c.servers) > 0 {
		out := c.servers
		c.mu.Unlock()
		return out
	}
	c.mu.Unlock()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://all.api.radio-browser.info/json/servers", nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck // defer close
		var raw []struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&raw); err == nil && len(raw) > 0 {
			out := make([]string, 0, len(raw))
			for _, s := range raw {
				out = append(out, "https://"+s.Name)
			}
			c.mu.Lock()
			c.servers = out
			c.serversFetched = time.Now()
			c.mu.Unlock()
			return out
		}
	}
	c.mu.Lock()
	c.servers = fallbackServers
	c.serversFetched = time.Now()
	c.mu.Unlock()
	return fallbackServers
}

// baseURL picks a mirror at random from the discovered set. Random rather
// than round-robin so two parallel requests don't slam the same mirror.
func (c *Client) baseURL(ctx context.Context) string {
	servers := c.discoverServers(ctx)
	if len(servers) == 0 {
		return fallbackServers[0]
	}
	return servers[rand.IntN(len(servers))] //nolint:gosec // non-crypto pick is fine here
}

// apiGet fetches one path with cache + memoization. The cache key is the
// path + sorted query string so different orderings collapse to one entry.
func (c *Client) apiGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	cacheKey := path + "?" + params.Encode()
	c.mu.Lock()
	if entry, ok := c.data[cacheKey]; ok && time.Now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.body, nil
	}
	c.mu.Unlock()

	u, err := url.Parse(c.baseURL(ctx) + path)
	if err != nil {
		return nil, fmt.Errorf("bad upstream URL: %w", err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("radio-browser fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer close

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radio-browser %s: HTTP %d", path, resp.StatusCode)
	}

	body, err := readAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	c.mu.Lock()
	c.data[cacheKey] = cacheEntry{body: body, expires: time.Now().Add(dataCacheTTL)}
	c.mu.Unlock()
	return body, nil
}

// SearchParams holds the filter fields for /json/stations/search. Empty
// strings are omitted so the upstream wildcard semantics apply.
type SearchParams struct {
	Name        string
	Tag         string
	Country     string
	CountryCode string
	Limit       int
	Offset      int
}

// Search runs a station search. Sorted by votes descending (matches the
// hibiki defaults) so the FE gets the high-quality stations first.
func (c *Client) Search(ctx context.Context, in SearchParams) ([]Station, error) {
	if in.Limit <= 0 || in.Limit > 200 {
		in.Limit = 30
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", in.Limit))
	q.Set("offset", fmt.Sprintf("%d", in.Offset))
	q.Set("order", "votes")
	q.Set("reverse", "true")
	q.Set("hidebroken", "true")
	if in.Name != "" {
		q.Set("name", in.Name)
	}
	if in.Tag != "" {
		q.Set("tag", in.Tag)
	}
	if in.Country != "" {
		q.Set("country", in.Country)
	}
	if in.CountryCode != "" {
		q.Set("countrycode", in.CountryCode)
	}
	body, err := c.apiGet(ctx, "/json/stations/search", q)
	if err != nil {
		return nil, err
	}
	var out []Station
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode stations: %w", err)
	}
	return out, nil
}

// TopCategory is one of the curated top-N endpoints.
type TopCategory string

const (
	TopVote    TopCategory = "topvote"
	TopClick   TopCategory = "topclick"
	LastChange TopCategory = "lastchange"
)

// Top returns the curated top-N list for one of the radio-browser
// categories. Bounded count so callers can't spider the upstream.
func (c *Client) Top(ctx context.Context, category TopCategory, count int) ([]Station, error) {
	if count <= 0 || count > 200 {
		count = 30
	}
	q := url.Values{}
	q.Set("hidebroken", "true")
	body, err := c.apiGet(ctx, fmt.Sprintf("/json/stations/%s/%d", category, count), q)
	if err != nil {
		return nil, err
	}
	var out []Station
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode top: %w", err)
	}
	return out, nil
}

// Countries returns every country with at least one station, sorted by
// station count descending. The FE shows this as a country picker.
func (c *Client) Countries(ctx context.Context) ([]Country, error) {
	q := url.Values{}
	q.Set("order", "stationcount")
	q.Set("reverse", "true")
	body, err := c.apiGet(ctx, "/json/countries", q)
	if err != nil {
		return nil, err
	}
	var out []Country
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode countries: %w", err)
	}
	return out, nil
}

// Tags returns the top-N tag list (genres, moods, eras, formats — radio-
// browser doesn't formalize the categories).
func (c *Client) Tags(ctx context.Context, limit int) ([]Tag, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := url.Values{}
	q.Set("order", "stationcount")
	q.Set("reverse", "true")
	q.Set("limit", fmt.Sprintf("%d", limit))
	body, err := c.apiGet(ctx, "/json/tags", q)
	if err != nil {
		return nil, err
	}
	var out []Tag
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	return out, nil
}

// PostClick fires a fire-and-forget click event so radio-browser's
// crowd-sourced popularity ranking sees our user's plays. Errors are
// swallowed — the upstream stats degrade gracefully when we miss a beat.
func (c *Client) PostClick(ctx context.Context, uuid string) {
	//nolint:gosec // G118: detached ctx is intentional — fire-and-forget click telemetry
	go func() {
		baseURL := c.baseURL(ctx)
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/json/url/"+url.PathEscape(uuid), nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := c.http.Do(req)
		if err != nil {
			return
		}
		_ = resp.Body.Close()
	}()
}

// readAll is a tiny wrapper around io.ReadAll that doesn't pull in the
// `io` import at the top — keeps the package surface minimal.
func readAll(r interface {
	Read(p []byte) (n int, err error)
}) ([]byte, error) {
	const chunk = 4096
	buf := make([]byte, 0, chunk)
	tmp := make([]byte, chunk)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}
