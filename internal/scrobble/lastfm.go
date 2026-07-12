package scrobble

import (
	"context"
	"crypto/md5" //nolint:gosec // Last.fm's api_sig algorithm mandates MD5
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

const lastfmBase = "https://ws.audioscrobbler.com/2.0/"

// LastFM is a client for the Last.fm API. APIKey/Secret are the server-level
// application credentials (HEYA_LASTFM_API_KEY/SECRET); SessionKey is the
// per-user credential from the auth handshake and only needed for writes.
// History reads (import) work with just the API key on public profiles.
type LastFM struct {
	APIKey     string
	Secret     string
	SessionKey string
	HTTP       *http.Client
}

func (c *LastFM) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// sign computes the api_sig Last.fm requires on authenticated calls:
// md5 of concatenated key+value pairs (sorted by key) + secret.
func (c *LastFM) sign(params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "format" || k == "callback" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for _, k := range keys {
		b = append(b, k...)
		b = append(b, params.Get(k)...)
	}
	b = append(b, c.Secret...)
	sum := md5.Sum(b) //nolint:gosec // spec-mandated
	return hex.EncodeToString(sum[:])
}

func (c *LastFM) call(ctx context.Context, method string, params url.Values, signed bool, out any) error {
	params.Set("method", method)
	params.Set("api_key", c.APIKey)
	if signed {
		params.Set("api_sig", c.sign(params))
	}
	params.Set("format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lastfmBase, nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = params.Encode()
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var apiErr struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &apiErr) == nil && apiErr.Error != 0 {
		return fmt.Errorf("last.fm %s: %s (code %d)", method, apiErr.Message, apiErr.Error)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// GetSession exchanges an authorized request token (the user approved it at
// last.fm/api/auth) for a permanent session key + username.
func (c *LastFM) GetSession(ctx context.Context, token string) (sessionKey, username string, err error) {
	params := url.Values{"token": {token}}
	var out struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := c.call(ctx, "auth.getSession", params, true, &out); err != nil {
		return "", "", err
	}
	return out.Session.Key, out.Session.Name, nil
}

// GetToken starts the desktop auth flow: the user opens
// https://www.last.fm/api/auth/?api_key=KEY&token=TOKEN, approves, then the
// server calls GetSession with the same token.
func (c *LastFM) GetToken(ctx context.Context) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.call(ctx, "auth.getToken", url.Values{}, true, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// RecentTracks fetches one page of a user's scrobble history (newest first).
// Page is 1-based. to (unix seconds, 0 = none) bounds results to scrobbles at
// or before that instant — the resume cursor for deep-history imports, which
// keeps every request on a shallow page instead of paginating hundreds deep
// (where Last.fm's API reliably starts failing with transient code 8).
func (c *LastFM) RecentTracks(ctx context.Context, user string, page, limit int, to int64) ([]Listen, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	params := url.Values{
		"user":  {user},
		"page":  {strconv.Itoa(page)},
		"limit": {strconv.Itoa(limit)},
	}
	if to > 0 {
		params.Set("to", strconv.FormatInt(to, 10))
	}
	var out struct {
		RecentTracks struct {
			Attr struct {
				TotalPages string `json:"totalPages"`
			} `json:"@attr"`
			Track []struct {
				Name   string `json:"name"`
				MBID   string `json:"mbid"`
				Artist struct {
					Text string `json:"#text"`
				} `json:"artist"`
				Album struct {
					Text string `json:"#text"`
				} `json:"album"`
				Date struct {
					UTS string `json:"uts"`
				} `json:"date"`
				Attr *struct {
					NowPlaying string `json:"nowplaying"`
				} `json:"@attr"`
			} `json:"track"`
		} `json:"recenttracks"`
	}
	if err := c.call(ctx, "user.getRecentTracks", params, false, &out); err != nil {
		return nil, 0, err
	}
	totalPages, _ := strconv.Atoi(out.RecentTracks.Attr.TotalPages)
	listens := make([]Listen, 0, len(out.RecentTracks.Track))
	for _, t := range out.RecentTracks.Track {
		if t.Attr != nil && t.Attr.NowPlaying == "true" {
			continue // the in-flight track has no timestamp
		}
		uts, err := strconv.ParseInt(t.Date.UTS, 10, 64)
		if err != nil {
			continue
		}
		listens = append(listens, Listen{
			ArtistName:    t.Artist.Text,
			TrackName:     t.Name,
			ReleaseName:   t.Album.Text,
			RecordingMBID: t.MBID, // last.fm's track mbid IS a recording mbid when present
			ListenedAt:    time.Unix(uts, 0).UTC(),
		})
	}
	return listens, totalPages, nil
}

// LovedTracks fetches one page of the user's loved tracks (api_key only,
// public data). Page is 1-based; returns loves plus total pages.
func (c *LastFM) LovedTracks(ctx context.Context, user string, page int) ([]Listen, int, error) {
	params := url.Values{
		"user":  {user},
		"page":  {strconv.Itoa(page)},
		"limit": {"200"},
	}
	var out struct {
		LovedTracks struct {
			Attr struct {
				TotalPages string `json:"totalPages"`
			} `json:"@attr"`
			Track []struct {
				Name   string `json:"name"`
				MBID   string `json:"mbid"`
				Artist struct {
					Name string `json:"name"`
				} `json:"artist"`
				Date struct {
					UTS string `json:"uts"`
				} `json:"date"`
			} `json:"track"`
		} `json:"lovedtracks"`
	}
	if err := c.call(ctx, "user.getLovedTracks", params, false, &out); err != nil {
		return nil, 0, err
	}
	totalPages, _ := strconv.Atoi(out.LovedTracks.Attr.TotalPages)
	loves := make([]Listen, 0, len(out.LovedTracks.Track))
	for _, t := range out.LovedTracks.Track {
		uts, _ := strconv.ParseInt(t.Date.UTS, 10, 64)
		loves = append(loves, Listen{
			ArtistName:    t.Artist.Name,
			TrackName:     t.Name,
			RecordingMBID: t.MBID,
			ListenedAt:    time.Unix(uts, 0).UTC(),
		})
	}
	return loves, totalPages, nil
}

// Scrobble submits up to 50 listens with the user's session key.
func (c *LastFM) Scrobble(ctx context.Context, listens []Listen) error {
	if len(listens) == 0 {
		return nil
	}
	if len(listens) > 50 {
		listens = listens[:50]
	}
	params := url.Values{"sk": {c.SessionKey}}
	for i, l := range listens {
		idx := "[" + strconv.Itoa(i) + "]"
		params.Set("artist"+idx, l.ArtistName)
		params.Set("track"+idx, l.TrackName)
		params.Set("timestamp"+idx, strconv.FormatInt(l.ListenedAt.Unix(), 10))
		if l.ReleaseName != "" {
			params.Set("album"+idx, l.ReleaseName)
		}
		if l.DurationSec > 0 {
			params.Set("duration"+idx, strconv.Itoa(l.DurationSec))
		}
		if l.RecordingMBID != "" {
			params.Set("mbid"+idx, l.RecordingMBID)
		}
	}
	return c.call(ctx, "track.scrobble", params, true, nil)
}
