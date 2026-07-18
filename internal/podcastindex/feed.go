package podcastindex

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/karbowiak/heya/internal/publichttp"
	"github.com/karbowiak/heya/internal/safedial"
	"github.com/mmcdole/gofeed"
	"golang.org/x/sync/singleflight"
)

// PodcastEpisode is the FE-friendly projection of one RSS <item>.
type PodcastEpisode struct {
	GUID          string `json:"guid"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	PubDate       string `json:"pub_date"`
	DurationSecs  int    `json:"duration_secs"`
	AudioURL      string `json:"audio_url"`
	AudioType     string `json:"audio_type"`
	AudioSize     int64  `json:"audio_size"`
	EpisodeNumber *int   `json:"episode_number,omitempty"`
	SeasonNumber  *int   `json:"season_number,omitempty"`
	ArtworkURL    string `json:"artwork_url,omitempty"`
}

// PodcastDetail is the parsed-feed result the FE renders as the podcast
// detail page (one big tile + an episode list).
type PodcastDetail struct {
	FeedURL     string           `json:"feed_url"`
	Title       string           `json:"title"`
	Author      string           `json:"author"`
	Description string           `json:"description"`
	ArtworkURL  string           `json:"artwork_url"`
	Link        string           `json:"link"`
	Language    string           `json:"language"`
	Categories  []string         `json:"categories"`
	Episodes    []PodcastEpisode `json:"episodes"`
}

type feedCacheEntry struct {
	detail  *PodcastDetail
	expires time.Time
	stored  time.Time
	weight  int64
}

const (
	feedCacheTTL              = 30 * time.Minute
	feedFetchTimeout          = 15 * time.Second
	maxFeedCacheEntries       = 128
	maxFeedCacheBytes         = int64(96 << 20)
	maxFeedFetches            = 4
	maxFeedBytes        int64 = 8 << 20
	maxFeedEpisodes           = 1000
	maxFeedCategories         = 64

	maxFeedURLBytes         = 8 << 10
	maxFeedTitleBytes       = 1 << 10
	maxFeedShortTextBytes   = 4 << 10
	maxFeedDescriptionBytes = 32 << 10
)

type feedFetcher struct {
	http *publichttp.Fetcher
	now  func() time.Time

	mu    sync.Mutex
	cache map[string]feedCacheEntry
	bytes int64
	group singleflight.Group

	maxCacheEntries int
	maxCacheBytes   int64
	fetchSlots      chan struct{}
}

func newFeedFetcher(fetcher *publichttp.Fetcher) *feedFetcher {
	if fetcher == nil {
		fetcher = publichttp.NewFetcher(feedFetchTimeout)
	}
	return &feedFetcher{
		http:            fetcher,
		now:             time.Now,
		cache:           make(map[string]feedCacheEntry),
		maxCacheEntries: maxFeedCacheEntries,
		maxCacheBytes:   maxFeedCacheBytes,
		fetchSlots:      make(chan struct{}, maxFeedFetches),
	}
}

var defaultFeedFetcher = newFeedFetcher(nil)

// htmlTagRe peels HTML tags out of <description> bodies. Most podcast feeds
// inline HTML in descriptions; we surface plain text. Entity decoding is
// done after via html.UnescapeString.
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	out := htmlTagRe.ReplaceAllString(s, "")
	out = html.UnescapeString(out)
	return strings.TrimSpace(out)
}

func boundedText(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// parseDuration handles both "1234" (seconds) and "HH:MM:SS" / "MM:SS"
// styles the iTunes spec allows. Returns 0 on anything unrecognized.
func parseDuration(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n >= 0 {
			return n
		}
		return 0
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	out := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || out > (int(^uint(0)>>1)-n)/60 {
			return 0
		}
		out = out*60 + n
	}
	return out
}

// FetchFeed downloads and parses one podcast RSS feed. Cached for
// feedCacheTTL — repeat visits to the same detail page are near-instant.
//
// gofeed handles RSS 2.0 + Atom + the iTunes extension. We surface the
// iTunes fields when present, fall back to plain RSS otherwise.
func FetchFeed(ctx context.Context, feedURL string) (*PodcastDetail, error) {
	return defaultFeedFetcher.Fetch(ctx, feedURL)
}

func (f *feedFetcher) Fetch(ctx context.Context, rawURL string) (*PodcastDetail, error) {
	cacheKey, err := normalizedFeedURL(rawURL)
	if err != nil {
		return nil, err
	}
	if detail, ok := f.cached(cacheKey); ok {
		return detail, nil
	}

	result := f.group.DoChan(cacheKey, func() (any, error) {
		// A request may have populated the cache while this caller joined the
		// flight but before its function started.
		if detail, ok := f.cached(cacheKey); ok {
			return detail, nil
		}
		// Do not let the first browser disconnect cancel a fetch now shared by
		// other callers. publichttp still bounds the detached operation.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), feedFetchTimeout)
		defer cancel()
		return f.fetch(fetchCtx, cacheKey)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case value := <-result:
		if value.Err != nil {
			return nil, value.Err
		}
		return value.Val.(*PodcastDetail), nil
	}
}

func (f *feedFetcher) fetch(ctx context.Context, feedURL string) (*PodcastDetail, error) {
	select {
	case f.fetchSlots <- struct{}{}:
		defer func() { <-f.fetchSlots }()
	default:
		return nil, fmt.Errorf("too many concurrent podcast feed fetches")
	}
	headers := make(http.Header)
	headers.Set("User-Agent", userAgent)
	response, err := f.http.Get(ctx, feedURL, maxFeedBytes, headers)
	if err != nil {
		return nil, fmt.Errorf("feed fetch: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed fetch returned HTTP %d", response.StatusCode)
	}
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(response.Body))
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	detail := &PodcastDetail{
		FeedURL:     feedURL,
		Title:       boundedText(parsed.Title, maxFeedTitleBytes),
		Description: boundedText(stripHTML(parsed.Description), maxFeedDescriptionBytes),
		Link:        boundedText(parsed.Link, maxFeedURLBytes),
		Language:    boundedText(parsed.Language, maxFeedShortTextBytes),
	}
	if parsed.ITunesExt != nil {
		detail.Author = boundedText(parsed.ITunesExt.Author, maxFeedShortTextBytes)
		if parsed.ITunesExt.Image != "" {
			detail.ArtworkURL = boundedText(parsed.ITunesExt.Image, maxFeedURLBytes)
		}
		for _, cat := range parsed.ITunesExt.Categories {
			if cat != nil && cat.Text != "" {
				detail.Categories = append(detail.Categories, boundedText(cat.Text, maxFeedTitleBytes))
				if len(detail.Categories) >= maxFeedCategories {
					break
				}
			}
		}
	}
	if detail.Author == "" && parsed.Author != nil {
		detail.Author = boundedText(parsed.Author.Name, maxFeedShortTextBytes)
	}
	if detail.ArtworkURL == "" && parsed.Image != nil {
		detail.ArtworkURL = boundedText(parsed.Image.URL, maxFeedURLBytes)
	}

	for _, item := range parsed.Items {
		ep := PodcastEpisode{
			GUID:        boundedText(item.GUID, maxFeedShortTextBytes),
			Title:       boundedText(item.Title, maxFeedTitleBytes),
			Description: boundedText(stripHTML(item.Description), maxFeedDescriptionBytes),
			PubDate:     boundedText(item.Published, maxFeedShortTextBytes),
		}
		// Audio enclosure — pick the first audio/* enclosure if multiple.
		for _, enc := range item.Enclosures {
			if enc == nil {
				continue
			}
			if enc.Type == "" || strings.HasPrefix(enc.Type, "audio/") {
				ep.AudioURL = boundedText(enc.URL, maxFeedURLBytes)
				ep.AudioType = boundedText(enc.Type, maxFeedShortTextBytes)
				if enc.Length != "" {
					if n, err := strconv.ParseInt(enc.Length, 10, 64); err == nil && n >= 0 {
						ep.AudioSize = n
					}
				}
				break
			}
		}
		if ep.AudioURL == "" {
			continue // skip non-audio items
		}
		// iTunes-specific fields.
		if item.ITunesExt != nil {
			ep.DurationSecs = parseDuration(boundedText(item.ITunesExt.Duration, maxFeedShortTextBytes))
			if item.ITunesExt.Image != "" {
				ep.ArtworkURL = boundedText(item.ITunesExt.Image, maxFeedURLBytes)
			}
			if item.ITunesExt.Episode != "" {
				if n, err := strconv.Atoi(item.ITunesExt.Episode); err == nil && n > 0 {
					ep.EpisodeNumber = &n
				}
			}
			if item.ITunesExt.Season != "" {
				if n, err := strconv.Atoi(item.ITunesExt.Season); err == nil && n > 0 {
					ep.SeasonNumber = &n
				}
			}
		}
		detail.Episodes = append(detail.Episodes, ep)
		if len(detail.Episodes) >= maxFeedEpisodes {
			break
		}
	}

	f.store(feedURL, detail)
	return detail, nil
}

func normalizedFeedURL(rawURL string) (string, error) {
	if len(rawURL) == 0 || len(rawURL) > maxFeedURLBytes {
		return "", fmt.Errorf("feed URL must be between 1 and %d bytes", maxFeedURLBytes)
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse feed URL: %w", err)
	}
	if err := safedial.ValidateHTTPURL(target); err != nil {
		return "", fmt.Errorf("validate feed URL: %w", err)
	}
	target.Fragment = ""
	return target.String(), nil
}

func (f *feedFetcher) cached(key string) (*PodcastDetail, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry, ok := f.cache[key]
	if !ok {
		return nil, false
	}
	if !f.now().Before(entry.expires) {
		f.bytes -= entry.weight
		delete(f.cache, key)
		return nil, false
	}
	return entry.detail, true
}

func (f *feedFetcher) store(key string, detail *PodcastDetail) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	for cachedKey, entry := range f.cache {
		if !now.Before(entry.expires) {
			f.bytes -= entry.weight
			delete(f.cache, cachedKey)
		}
	}
	if previous, exists := f.cache[key]; exists {
		f.bytes -= previous.weight
		delete(f.cache, key)
	}
	weight := podcastDetailWeight(detail)
	if weight > f.maxCacheBytes {
		return
	}
	for len(f.cache) >= f.maxCacheEntries || f.bytes+weight > f.maxCacheBytes {
		if !f.evictOldestLocked() {
			break
		}
	}
	f.cache[key] = feedCacheEntry{detail: detail, expires: now.Add(feedCacheTTL), stored: now, weight: weight}
	f.bytes += weight
}

func (f *feedFetcher) evictOldestLocked() bool {
	oldestKey := ""
	var oldestTime time.Time
	for key, entry := range f.cache {
		if oldestKey == "" || entry.stored.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.stored
		}
	}
	if oldestKey == "" {
		return false
	}
	f.bytes -= f.cache[oldestKey].weight
	delete(f.cache, oldestKey)
	return true
}

func podcastDetailWeight(detail *PodcastDetail) int64 {
	if detail == nil {
		return 0
	}
	weight := int64(256 + len(detail.FeedURL) + len(detail.Title) + len(detail.Author) + len(detail.Description) + len(detail.ArtworkURL) + len(detail.Link) + len(detail.Language))
	for _, category := range detail.Categories {
		weight += int64(16 + len(category))
	}
	for _, episode := range detail.Episodes {
		weight += int64(192 + len(episode.GUID) + len(episode.Title) + len(episode.Description) + len(episode.PubDate) + len(episode.AudioURL) + len(episode.AudioType) + len(episode.ArtworkURL))
	}
	return weight
}
