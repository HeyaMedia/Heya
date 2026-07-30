// Package torznab speaks the Torznab/Newznab XML API — the lingua franca of
// release indexers. One client covers Usenet indexers natively, every indexer
// behind Prowlarr/Jackett (both expose per-indexer Torznab endpoints), and
// future native tracker definitions.
package torznab

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	// BaseURL is the api endpoint itself (e.g. Prowlarr's
	// https://host/1/api or a Newznab indexer's https://host/api).
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Caps is the parsed t=caps response: what the indexer can search and which
// category tree it exposes.
type Caps struct {
	ServerTitle string
	Searching   []SearchMode
	Categories  []Category
}

type SearchMode struct {
	Name            string // "search", "tv-search", "movie-search", "music-search", "book-search"
	Available       bool
	SupportedParams []string
}

type Category struct {
	ID      int
	Name    string
	Subcats []Category
}

type capsXML struct {
	Server struct {
		Title string `xml:"title,attr"`
	} `xml:"server"`
	Searching struct {
		Search      searchModeXML `xml:"search"`
		TVSearch    searchModeXML `xml:"tv-search"`
		MovieSearch searchModeXML `xml:"movie-search"`
		MusicSearch searchModeXML `xml:"music-search"`
		BookSearch  searchModeXML `xml:"book-search"`
	} `xml:"searching"`
	Categories struct {
		Category []categoryXML `xml:"category"`
	} `xml:"categories"`
}

type searchModeXML struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type categoryXML struct {
	ID     string        `xml:"id,attr"`
	Name   string        `xml:"name,attr"`
	Subcat []categoryXML `xml:"subcat"`
}

func (c *Client) Caps(ctx context.Context) (*Caps, error) {
	body, err := c.get(ctx, url.Values{"t": {"caps"}})
	if err != nil {
		return nil, err
	}
	var raw capsXML
	if err := xml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("torznab caps: parsing response: %w", err)
	}
	caps := &Caps{ServerTitle: raw.Server.Title}
	modes := []struct {
		name string
		xml  searchModeXML
	}{
		{"search", raw.Searching.Search},
		{"tv-search", raw.Searching.TVSearch},
		{"movie-search", raw.Searching.MovieSearch},
		{"music-search", raw.Searching.MusicSearch},
		{"book-search", raw.Searching.BookSearch},
	}
	for _, m := range modes {
		mode := SearchMode{Name: m.name, Available: m.xml.Available == "yes"}
		if m.xml.SupportedParams != "" {
			mode.SupportedParams = strings.Split(m.xml.SupportedParams, ",")
		}
		caps.Searching = append(caps.Searching, mode)
	}
	for _, cat := range raw.Categories.Category {
		caps.Categories = append(caps.Categories, convertCategory(cat))
	}
	return caps, nil
}

func convertCategory(raw categoryXML) Category {
	cat := Category{Name: raw.Name}
	cat.ID, _ = strconv.Atoi(raw.ID)
	for _, sub := range raw.Subcat {
		cat.Subcats = append(cat.Subcats, convertCategory(sub))
	}
	return cat
}

// Query is one search request. Zero-value fields are omitted from the call.
// Season/Episode are pointers so an explicit zero survives — specials live in
// season 0 and `S00E05` must emit season=0, not drop the constraint.
type Query struct {
	// Type is the torznab search mode: "search", "tvsearch", "movie", "music", "book".
	Type    string
	Q       string
	Cats    []int
	ImdbID  string
	TmdbID  string
	TvdbID  string
	Season  *int
	Episode *int
	Artist  string
	Album   string
	Limit   int
	Offset  int
}

// Attr is one raw torznab:attr/newznab:attr pair, order-preserved. Repeated
// names are legal (e.g. multiple category attrs) and must not collapse.
type Attr struct {
	Name  string
	Value string
}

// Release is one search result item, protocol-agnostic: torznab:attr and
// newznab:attr both land in Attrs (last-wins flattened) and RawAttrs
// (ordered, duplicates preserved — the accountability record).
type Release struct {
	Title       string
	GUID        string
	Size        int64
	PublishDate time.Time
	// PublishDateRaw carries the unparsed pubDate text when no known layout
	// matched, so the failure is visible instead of a silent zero time.
	PublishDateRaw string
	DownloadURL    string
	InfoURL        string
	Categories     []int
	// RawCategories keeps non-numeric <category> element text (site-specific
	// names) that can't map to newznab ids.
	RawCategories []string
	Seeders       int
	Peers         int
	Attrs         map[string]string
	RawAttrs      []Attr
}

type rssXML struct {
	Channel struct {
		Items []itemXML `xml:"item"`
	} `xml:"channel"`
}

type itemXML struct {
	Title    string `xml:"title"`
	GUID     string `xml:"guid"`
	Link     string `xml:"link"`
	Comments string `xml:"comments"`
	PubDate  string `xml:"pubDate"`
	Size     int64  `xml:"size"`
	// Plain RSS <category> elements — some feeds carry ids here without
	// mirroring them into attrs.
	Category  []string `xml:"category"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
	} `xml:"enclosure"`
	// Matches torznab:attr and newznab:attr alike — encoding/xml matches on
	// the local name when the tag carries no namespace.
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

func (c *Client) Search(ctx context.Context, q Query) ([]Release, error) {
	params := url.Values{}
	searchType := q.Type
	if searchType == "" {
		searchType = "search"
	}
	params.Set("t", searchType)
	if q.Q != "" {
		params.Set("q", q.Q)
	}
	if len(q.Cats) > 0 {
		cats := make([]string, len(q.Cats))
		for i, cat := range q.Cats {
			cats[i] = strconv.Itoa(cat)
		}
		params.Set("cat", strings.Join(cats, ","))
	}
	if q.ImdbID != "" {
		params.Set("imdbid", strings.TrimPrefix(q.ImdbID, "tt"))
	}
	if q.TmdbID != "" {
		params.Set("tmdbid", q.TmdbID)
	}
	if q.TvdbID != "" {
		params.Set("tvdbid", q.TvdbID)
	}
	if q.Season != nil {
		params.Set("season", strconv.Itoa(*q.Season))
	}
	if q.Episode != nil {
		params.Set("ep", strconv.Itoa(*q.Episode))
	}
	if q.Artist != "" {
		params.Set("artist", q.Artist)
	}
	if q.Album != "" {
		params.Set("album", q.Album)
	}
	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		params.Set("offset", strconv.Itoa(q.Offset))
	}

	body, err := c.get(ctx, params)
	if err != nil {
		return nil, err
	}
	var raw rssXML
	if err := xml.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("torznab search: parsing response: %w", err)
	}

	releases := make([]Release, 0, len(raw.Channel.Items))
	for _, item := range raw.Channel.Items {
		rel := Release{
			Title:       item.Title,
			GUID:        item.GUID,
			Size:        item.Size,
			DownloadURL: item.Link,
			InfoURL:     item.Comments,
			Attrs:       make(map[string]string, len(item.Attrs)),
			RawAttrs:    make([]Attr, 0, len(item.Attrs)),
		}
		if rel.DownloadURL == "" {
			rel.DownloadURL = item.Enclosure.URL
		}
		if rel.Size == 0 {
			rel.Size = item.Enclosure.Length
		}
		rel.PublishDate, rel.PublishDateRaw = parsePubDate(item.PubDate)
		seenCats := make(map[int]bool)
		addCat := func(id int) {
			if !seenCats[id] {
				seenCats[id] = true
				rel.Categories = append(rel.Categories, id)
			}
		}
		// Some feeds put category ids only in plain <category> elements and
		// never mirror them into attrs; parse both representations.
		for _, cat := range item.Category {
			cat = strings.TrimSpace(cat)
			if cat == "" {
				continue
			}
			if id, err := strconv.Atoi(cat); err == nil {
				addCat(id)
			} else {
				rel.RawCategories = append(rel.RawCategories, cat)
			}
		}
		for _, attr := range item.Attrs {
			rel.Attrs[attr.Name] = attr.Value
			rel.RawAttrs = append(rel.RawAttrs, Attr{Name: attr.Name, Value: attr.Value})
			switch attr.Name {
			case "category":
				if id, err := strconv.Atoi(attr.Value); err == nil {
					addCat(id)
				}
			case "seeders":
				rel.Seeders, _ = strconv.Atoi(attr.Value)
			case "peers":
				rel.Peers, _ = strconv.Atoi(attr.Value)
			case "size":
				if rel.Size == 0 {
					rel.Size, _ = strconv.ParseInt(attr.Value, 10, 64)
				}
			}
		}
		releases = append(releases, rel)
	}
	return releases, nil
}

// pubDateLayouts covers the formats real Newznab/Prowlarr feeds emit; RFC1123Z
// is the spec, the rest show up in the wild.
var pubDateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 -0700",
}

// parsePubDate returns the parsed time, or the zero time plus the raw text
// when nothing matched — a recorded failure, not a silent one.
func parsePubDate(raw string) (time.Time, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, ""
	}
	for _, layout := range pubDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, ""
		}
	}
	return time.Time{}, raw
}

// errorXML is the torznab error envelope: <error code="100" description="..."/>.
type errorXML struct {
	XMLName     xml.Name `xml:"error"`
	Code        string   `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

func (c *Client) get(ctx context.Context, params url.Values) ([]byte, error) {
	if c.APIKey != "" {
		params.Set("apikey", c.APIKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("torznab: building request: %w", err)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Torznab authenticates via ?apikey= and url.Error embeds the full
		// request URL — strip the query so the key can't ride the error into
		// persisted test results or response bodies. The chain stays intact
		// for errors.Is(context.Canceled) and friends.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			if u, perr := url.Parse(uerr.URL); perr == nil && u.RawQuery != "" {
				u.RawQuery = ""
				uerr.URL = u.String()
			}
		}
		return nil, fmt.Errorf("torznab: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // defer close
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("torznab: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torznab: %s returned HTTP %d", req.URL.Host, resp.StatusCode)
	}
	// Indexers report auth/param failures as 200s with an <error> document.
	var apiErr errorXML
	if err := xml.Unmarshal(body, &apiErr); err == nil && apiErr.Description != "" {
		return nil, fmt.Errorf("torznab: indexer error %s: %s", apiErr.Code, apiErr.Description)
	}
	return body, nil
}
