package podcastindex

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"
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

// feedCache memoizes the slow RSS parses. TTL is short relative to a
// podcast's publishing cadence (30 min) — frequent visits to the same
// detail page don't re-parse, but new episodes still surface promptly.
var (
	feedCacheMu sync.Mutex
	feedCache   = map[string]feedCacheEntry{}
)

type feedCacheEntry struct {
	detail  *PodcastDetail
	expires time.Time
}

const feedCacheTTL = 30 * time.Minute

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

// parseDuration handles both "1234" (seconds) and "HH:MM:SS" / "MM:SS"
// styles the iTunes spec allows. Returns 0 on anything unrecognized.
func parseDuration(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	parts := strings.Split(raw, ":")
	out := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
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
	feedCacheMu.Lock()
	if entry, ok := feedCache[feedURL]; ok && time.Now().Before(entry.expires) {
		feedCacheMu.Unlock()
		return entry.detail, nil
	}
	feedCacheMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feed fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer close
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed %s: HTTP %d", feedURL, resp.StatusCode)
	}

	parsed, err := gofeed.NewParser().Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	detail := &PodcastDetail{
		FeedURL:     feedURL,
		Title:       parsed.Title,
		Description: stripHTML(parsed.Description),
		Link:        parsed.Link,
		Language:    parsed.Language,
	}
	if parsed.ITunesExt != nil {
		detail.Author = parsed.ITunesExt.Author
		if parsed.ITunesExt.Image != "" {
			detail.ArtworkURL = parsed.ITunesExt.Image
		}
		for _, cat := range parsed.ITunesExt.Categories {
			if cat != nil && cat.Text != "" {
				detail.Categories = append(detail.Categories, cat.Text)
			}
		}
	}
	if detail.Author == "" && parsed.Author != nil {
		detail.Author = parsed.Author.Name
	}
	if detail.ArtworkURL == "" && parsed.Image != nil {
		detail.ArtworkURL = parsed.Image.URL
	}

	for _, item := range parsed.Items {
		ep := PodcastEpisode{
			GUID:        item.GUID,
			Title:       item.Title,
			Description: stripHTML(item.Description),
			PubDate:     item.Published,
		}
		// Audio enclosure — pick the first audio/* enclosure if multiple.
		for _, enc := range item.Enclosures {
			if enc == nil {
				continue
			}
			if enc.Type == "" || strings.HasPrefix(enc.Type, "audio/") {
				ep.AudioURL = enc.URL
				ep.AudioType = enc.Type
				if enc.Length != "" {
					if n, err := strconv.ParseInt(enc.Length, 10, 64); err == nil {
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
			ep.DurationSecs = parseDuration(item.ITunesExt.Duration)
			if item.ITunesExt.Image != "" {
				ep.ArtworkURL = item.ITunesExt.Image
			}
			if item.ITunesExt.Episode != "" {
				if n, err := strconv.Atoi(item.ITunesExt.Episode); err == nil {
					ep.EpisodeNumber = &n
				}
			}
			if item.ITunesExt.Season != "" {
				if n, err := strconv.Atoi(item.ITunesExt.Season); err == nil {
					ep.SeasonNumber = &n
				}
			}
		}
		detail.Episodes = append(detail.Episodes, ep)
	}

	feedCacheMu.Lock()
	feedCache[feedURL] = feedCacheEntry{detail: detail, expires: time.Now().Add(feedCacheTTL)}
	feedCacheMu.Unlock()
	return detail, nil
}
