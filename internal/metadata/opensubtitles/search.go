package opensubtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

func (c *Client) Search(ctx context.Context, params SearchParams) (*SearchResponse, error) {
	q := url.Values{}
	if params.IMDbID != "" {
		id := strings.TrimPrefix(params.IMDbID, "tt")
		q.Set("imdb_id", id)
	}
	if params.TMDbID != "" {
		q.Set("tmdb_id", params.TMDbID)
	}
	if params.Query != "" {
		q.Set("query", params.Query)
	}
	if len(params.Languages) > 0 {
		q.Set("languages", strings.Join(params.Languages, ","))
	}
	if params.Season > 0 {
		q.Set("season_number", strconv.Itoa(params.Season))
	}
	if params.Episode > 0 {
		q.Set("episode_number", strconv.Itoa(params.Episode))
	}
	if params.Type != "" {
		q.Set("type", params.Type)
	}

	resp, err := c.doGet(ctx, "/subtitles?"+q.Encode())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed (status %d): %s", resp.StatusCode, string(b))
	}

	var result SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("search decode failed: %w", err)
	}
	return &result, nil
}

func (c *Client) Download(ctx context.Context, fileID int) (*DownloadResponse, error) {
	resp, err := c.doPost(ctx, "/download", DownloadRequest{FileID: fileID})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(b))
	}

	var result DownloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("download decode failed: %w", err)
	}
	return &result, nil
}

func (c *Client) UserInfo(ctx context.Context) (*UserInfo, error) {
	resp, err := c.doGet(ctx, "/infos/user")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("user info failed (status %d): %s", resp.StatusCode, string(b))
	}

	var result UserInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("user info decode failed: %w", err)
	}
	return &result.Data, nil
}
