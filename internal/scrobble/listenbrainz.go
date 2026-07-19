// Package scrobble holds the external listen-history clients (ListenBrainz,
// Last.fm): validate credentials, page through a user's historical listens
// for import, and submit new listens (scrobbles). Pure HTTP clients — no DB,
// no service imports — so both the service layer and workers can use them.
package scrobble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const listenBrainzBase = "https://api.listenbrainz.org/1"

// Listen is one normalized historical listen from either service.
type Listen struct {
	ArtistName    string
	TrackName     string
	ReleaseName   string
	RecordingMBID string
	ListenedAt    time.Time
	DurationSec   int
}

// ListenBrainz is a per-user client (token-authenticated).
type ListenBrainz struct {
	Token string
	HTTP  *http.Client
}

func (c *ListenBrainz) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *ListenBrainz) req(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, listenBrainzBase+path, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Token "+c.Token)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	return r, nil
}

// ValidateToken checks the token and returns the ListenBrainz user name it
// belongs to.
func (c *ListenBrainz) ValidateToken(ctx context.Context) (string, error) {
	req, err := c.req(ctx, http.MethodGet, "/validate-token", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	var out struct {
		Valid    bool   `json:"valid"`
		UserName string `json:"user_name"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("listenbrainz validate: %w", err)
	}
	if !out.Valid {
		return "", fmt.Errorf("listenbrainz token invalid: %s", out.Message)
	}
	return out.UserName, nil
}

// Listens fetches one page of the user's listen history strictly OLDER than
// maxTS (pass time.Now() for the first page), newest first. Returns the
// listens plus the timestamp to pass as the next page's maxTS (zero time when
// the history is exhausted).
func (c *ListenBrainz) Listens(ctx context.Context, user string, maxTS time.Time, count int) ([]Listen, time.Time, error) {
	if count <= 0 || count > 100 {
		count = 100 // API page cap
	}
	q := url.Values{}
	q.Set("max_ts", fmt.Sprintf("%d", maxTS.Unix()))
	q.Set("count", fmt.Sprintf("%d", count))
	req, err := c.req(ctx, http.MethodGet, "/user/"+url.PathEscape(user)+"/listens?"+q.Encode(), nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, time.Time{}, fmt.Errorf("listenbrainz listens: HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Payload struct {
			Listens []struct {
				ListenedAt    int64 `json:"listened_at"`
				TrackMetadata struct {
					ArtistName     string `json:"artist_name"`
					TrackName      string `json:"track_name"`
					ReleaseName    string `json:"release_name"`
					AdditionalInfo struct {
						RecordingMBID string  `json:"recording_mbid"`
						DurationMS    float64 `json:"duration_ms"`
						Duration      float64 `json:"duration"`
					} `json:"additional_info"`
					MBIDMapping struct {
						RecordingMBID string `json:"recording_mbid"`
					} `json:"mbid_mapping"`
				} `json:"track_metadata"`
			} `json:"listens"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, time.Time{}, fmt.Errorf("listenbrainz listens: %w", err)
	}
	listens := make([]Listen, 0, len(out.Payload.Listens))
	oldest := time.Time{}
	for _, l := range out.Payload.Listens {
		md := l.TrackMetadata
		mbid := md.AdditionalInfo.RecordingMBID
		if mbid == "" {
			mbid = md.MBIDMapping.RecordingMBID // LB's own server-side match
		}
		dur := int(md.AdditionalInfo.Duration)
		if dur == 0 && md.AdditionalInfo.DurationMS > 0 {
			dur = int(md.AdditionalInfo.DurationMS / 1000)
		}
		at := time.Unix(l.ListenedAt, 0).UTC()
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
		listens = append(listens, Listen{
			ArtistName:    md.ArtistName,
			TrackName:     md.TrackName,
			ReleaseName:   md.ReleaseName,
			RecordingMBID: mbid,
			ListenedAt:    at,
			DurationSec:   dur,
		})
	}
	if len(listens) == 0 {
		return listens, time.Time{}, nil // exhausted
	}
	return listens, oldest, nil
}

// Feedback fetches one page of the user's recording feedback: score +1
// (love) and -1 (hate), with track metadata resolved server-side. Returns
// loves, hates, and the total feedback count for paging (offset-based).
func (c *ListenBrainz) Feedback(ctx context.Context, user string, offset, count int) (loves, hates []Listen, total int, err error) {
	if count <= 0 || count > 100 {
		count = 100
	}
	q := url.Values{}
	q.Set("metadata", "true")
	q.Set("count", fmt.Sprintf("%d", count))
	q.Set("offset", fmt.Sprintf("%d", offset))
	req, err := c.req(ctx, http.MethodGet, "/feedback/user/"+url.PathEscape(user)+"/get-feedback?"+q.Encode(), nil)
	if err != nil {
		return nil, nil, 0, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, nil, 0, fmt.Errorf("listenbrainz feedback: HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		TotalCount int `json:"total_count"`
		Feedback   []struct {
			RecordingMBID string `json:"recording_mbid"`
			Score         int    `json:"score"`
			Created       int64  `json:"created"`
			TrackMetadata *struct {
				ArtistName string `json:"artist_name"`
				TrackName  string `json:"track_name"`
			} `json:"track_metadata"`
		} `json:"feedback"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, nil, 0, fmt.Errorf("listenbrainz feedback: %w", err)
	}
	for _, f := range out.Feedback {
		l := Listen{RecordingMBID: f.RecordingMBID}
		// `created` is when the user gave the feedback — verified present on
		// real accounts back to 2011. Without it every imported love was
		// stamped with the import time, which both wrecked "loved on" dates
		// and broke the external_listens dedupe key across import runs
		// (each run minted a fresh now(), duplicating every love).
		if f.Created > 0 {
			l.ListenedAt = time.Unix(f.Created, 0).UTC()
		}
		if f.TrackMetadata != nil {
			l.ArtistName = f.TrackMetadata.ArtistName
			l.TrackName = f.TrackMetadata.TrackName
		}
		switch f.Score {
		case 1:
			loves = append(loves, l)
		case -1:
			hates = append(hates, l)
		}
	}
	return loves, hates, out.TotalCount, nil
}

// SubmitFeedback syncs a track reaction: score 1 = love, -1 = hate,
// 0 = clear. ListenBrainz keys feedback by recording MBID.
func (c *ListenBrainz) SubmitFeedback(ctx context.Context, recordingMBID string, score int) error {
	if recordingMBID == "" {
		return fmt.Errorf("recording mbid required for feedback")
	}
	raw, err := json.Marshal(map[string]any{"recording_mbid": recordingMBID, "score": score})
	if err != nil {
		return err
	}
	req, err := c.req(ctx, http.MethodPost, "/feedback/recording-feedback", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("listenbrainz feedback submit: HTTP %d: %s", resp.StatusCode, string(msg))
	}
	return nil
}

// Submit sends listens to ListenBrainz. listenType is "single" for live
// scrobbles or "import" for backfills; ListenBrainz caps batches, so callers
// should keep len(listens) modest (≤50).
func (c *ListenBrainz) Submit(ctx context.Context, listenType string, listens []Listen) error {
	type payloadListen struct {
		ListenedAt    int64 `json:"listened_at,omitempty"`
		TrackMetadata struct {
			ArtistName     string         `json:"artist_name"`
			TrackName      string         `json:"track_name"`
			ReleaseName    string         `json:"release_name,omitempty"`
			AdditionalInfo map[string]any `json:"additional_info,omitempty"`
		} `json:"track_metadata"`
	}
	body := struct {
		ListenType string          `json:"listen_type"`
		Payload    []payloadListen `json:"payload"`
	}{ListenType: listenType}
	for _, l := range listens {
		var p payloadListen
		if listenType != "playing_now" {
			p.ListenedAt = l.ListenedAt.Unix()
		}
		p.TrackMetadata.ArtistName = l.ArtistName
		p.TrackMetadata.TrackName = l.TrackName
		p.TrackMetadata.ReleaseName = l.ReleaseName
		info := map[string]any{"submission_client": "Heya"}
		if l.RecordingMBID != "" {
			info["recording_mbid"] = l.RecordingMBID
		}
		if l.DurationSec > 0 {
			info["duration"] = l.DurationSec
		}
		p.TrackMetadata.AdditionalInfo = info
		body.Payload = append(body.Payload, p)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := c.req(ctx, http.MethodPost, "/submit-listens", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("listenbrainz submit: HTTP %d: %s", resp.StatusCode, string(msg))
	}
	return nil
}
