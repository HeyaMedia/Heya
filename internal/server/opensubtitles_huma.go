package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata/opensubtitles"
	"github.com/karbowiak/heya/internal/service"
)

const maxSubtitleBytes = 10 << 20

// registerOpenSubtitlesRoutes mounts /api/opensubtitles/*. Credentials come
// from the system_settings KV under the "opensubtitles" key (admin-managed).
func registerOpenSubtitlesRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodPost, "/api/opensubtitles/test", "opensubtitles-test", "Test OpenSubtitles credentials", "OpenSubtitles")),
		func(ctx context.Context, in *struct {
			Body osCredentials
		}) (*JSONOutput[osTestBody], error) {
			if in.Body.APIKey == "" || in.Body.Username == "" || in.Body.Password == "" {
				return nil, huma.Error400BadRequest("api_key, username, and password required")
			}
			client := opensubtitles.NewClient(in.Body.APIKey)
			client.SetCredentials(in.Body.Username, in.Body.Password)
			if err := client.Login(ctx); err != nil {
				return &JSONOutput[osTestBody]{Body: osTestBody{OK: false, Error: err.Error()}}, nil
			}
			info, err := client.UserInfo(ctx)
			if err != nil {
				return &JSONOutput[osTestBody]{Body: osTestBody{OK: false, Error: err.Error()}}, nil
			}
			return &JSONOutput[osTestBody]{Body: osTestBody{OK: true, User: info}}, nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/opensubtitles/user-info", "opensubtitles-user-info", "Saved-credential user info", "OpenSubtitles")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[*opensubtitles.UserInfo], error) {
			client, err := loadOSClient(ctx, app)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			info, err := client.UserInfo(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(info), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/opensubtitles/search", "opensubtitles-search", "Search subtitles", "OpenSubtitles")),
		func(ctx context.Context, in *struct {
			IMDbID    string `query:"imdb_id" maxLength:"32"`
			TMDbID    string `query:"tmdb_id" maxLength:"32"`
			Query     string `query:"query" maxLength:"200"`
			Type      string `query:"type" enum:",movie,episode,all" doc:"Empty = unspecified"`
			Languages string `query:"languages" maxLength:"256" doc:"Comma-separated ISO codes"`
			Season    int    `query:"season" minimum:"0" maximum:"9999"`
			Episode   int    `query:"episode" minimum:"0" maximum:"9999"`
			MediaID   int64  `query:"media_id" minimum:"0" doc:"Inflate from a known media item"`
		}) (*JSONOutput[*opensubtitles.SearchResponse], error) {
			client, err := loadOSClient(ctx, app)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			params := opensubtitles.SearchParams{
				IMDbID:  in.IMDbID,
				TMDbID:  in.TMDbID,
				Query:   in.Query,
				Type:    in.Type,
				Season:  in.Season,
				Episode: in.Episode,
			}
			if in.Languages != "" {
				params.Languages = strings.Split(in.Languages, ",")
			}
			if in.MediaID > 0 {
				inflateFromMedia(ctx, app, in.MediaID, &params)
			}
			results, err := client.Search(ctx, params)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(results, 60), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/opensubtitles/download", "opensubtitles-download", "Download a subtitle to disk + attach as asset", "OpenSubtitles")),
		func(ctx context.Context, in *struct {
			Body struct {
				MediaItemID int64  `json:"media_item_id" minimum:"1"`
				FileID      int    `json:"file_id" minimum:"1"`
				Language    string `json:"language" maxLength:"16"`
				FileName    string `json:"file_name" maxLength:"256"`
			}
		}) (*JSONOutput[osDownloadBody], error) {
			if in.Body.MediaItemID == 0 || in.Body.FileID == 0 {
				return nil, huma.Error400BadRequest("media_item_id and file_id required")
			}
			client, err := loadOSClient(ctx, app)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			dl, err := client.Download(ctx, in.Body.FileID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			q := sqlc.New(app.DBPool())
			item, err := q.GetMediaItemByID(ctx, in.Body.MediaItemID)
			if err != nil {
				return nil, huma.Error404NotFound("media item not found")
			}

			dirName := item.Slug
			if dirName == "" {
				dirName = fmt.Sprintf("%d", item.ID)
			}
			subDir := filepath.Join(app.ConfigSnapshot().DataDir.Value, "subtitles", string(item.MediaType), dirName)
			if err := os.MkdirAll(subDir, 0o750); err != nil {
				return nil, huma.Error500InternalServerError("failed to create subtitles dir: " + err.Error())
			}
			filename := safeSubtitleFilename(dl.FileName)
			if filename == "" {
				filename = safeSubtitleFilename(in.Body.FileName)
			}
			if filename == "" {
				filename = fmt.Sprintf("%s.%s.srt", dirName, safeSubtitleLanguage(in.Body.Language))
			}
			// G304: destPath is built from server-controlled DataDir + validated
			// slug/id + filename; not a user-supplied path.
			destPath := filepath.Join(subDir, filename) //nolint:gosec

			downloadURL, err := safeSubtitleDownloadURL(dl.Link)
			if err != nil {
				return nil, huma.Error502BadGateway("invalid subtitle download URL")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
			if err != nil {
				return nil, huma.Error502BadGateway("invalid subtitle download URL")
			}
			httpClient := &http.Client{Timeout: 30 * time.Second}
			resp, err := httpClient.Do(req)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to download subtitle file")
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				return nil, huma.Error502BadGateway("subtitle download returned " + resp.Status)
			}

			tmp, err := os.CreateTemp(subDir, "."+filename+"-*")
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to save subtitle file")
			}
			tmpPath := tmp.Name()
			size, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, maxSubtitleBytes+1))
			closeErr := tmp.Close()
			if copyErr != nil || closeErr != nil {
				_ = os.Remove(tmpPath)
				return nil, huma.Error500InternalServerError("failed to save subtitle file")
			}
			if size > maxSubtitleBytes {
				_ = os.Remove(tmpPath)
				return nil, huma.Error400BadRequest("subtitle file is too large")
			}
			if err := os.Rename(tmpPath, destPath); err != nil {
				_ = os.Remove(tmpPath)
				return nil, huma.Error500InternalServerError("failed to save subtitle file")
			}

			asset, _ := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
				MediaItemID: in.Body.MediaItemID,
				AssetType:   sqlc.AssetTypeSubtitle,
				Source:      "opensubtitles",
				LocalPath:   destPath,
				Language:    in.Body.Language,
				FileSize:    size,
			})
			return &JSONOutput[osDownloadBody]{Body: osDownloadBody{
				Status:    "downloaded",
				Asset:     asset,
				Remaining: dl.Remaining,
			}}, nil
		})
}

func safeSubtitleFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return ""
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".srt", ".ass", ".ssa", ".vtt", ".sub":
		return name
	default:
		return ""
	}
}

func safeSubtitleLanguage(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return "und"
	}
	for _, r := range language {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return "und"
		}
	}
	return language
}

func safeSubtitleDownloadURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("subtitle URL must be absolute HTTPS")
	}
	return u.String(), nil
}

type osCredentials struct {
	APIKey   string `json:"api_key" doc:"OpenSubtitles API key"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type osTestBody struct {
	OK    bool   `json:"ok"`
	User  any    `json:"user,omitempty"`
	Error string `json:"error,omitempty"`
}

type osDownloadBody struct {
	Status    string          `json:"status"`
	Asset     sqlc.MediaAsset `json:"asset"`
	Remaining int             `json:"remaining"`
}

// loadOSClient pulls saved OpenSubtitles credentials from the system_settings
// KV and constructs a client. Returns an error if not configured.
func loadOSClient(ctx context.Context, app *service.App) (*opensubtitles.Client, error) {
	raw, err := app.GetSystemSetting(ctx, "opensubtitles")
	if err != nil {
		return nil, fmt.Errorf("opensubtitles not configured")
	}
	var creds osCredentials
	if err := json.Unmarshal(raw, &creds); err != nil || creds.APIKey == "" || creds.Username == "" {
		return nil, fmt.Errorf("opensubtitles credentials incomplete")
	}
	client := opensubtitles.NewClient(creds.APIKey)
	client.SetCredentials(creds.Username, creds.Password)
	return client, nil
}

// inflateFromMedia fills in IMDbID/TMDbID/Query/Type from a known media item
// so the search picks the right show without the caller passing every hint.
func inflateFromMedia(ctx context.Context, app *service.App, id int64, p *opensubtitles.SearchParams) {
	q := sqlc.New(app.DBPool())
	item, err := q.GetMediaItemByID(ctx, id)
	if err != nil {
		return
	}
	var externalIDs map[string]string
	if json.Unmarshal(item.ExternalIds, &externalIDs) == nil {
		if p.IMDbID == "" {
			p.IMDbID = externalIDs["imdb"]
		}
		if p.TMDbID == "" {
			p.TMDbID = externalIDs["tmdb"]
		}
	}
	if p.Query == "" {
		p.Query = item.Title
	}
	switch item.MediaType {
	case sqlc.MediaTypeMovie:
		p.Type = "movie"
	case sqlc.MediaTypeTv:
		p.Type = "episode"
	}
}
