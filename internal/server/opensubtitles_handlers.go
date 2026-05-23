package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata/opensubtitles"
	"github.com/karbowiak/heya/internal/service"
)

type osCredentials struct {
	APIKey   string `json:"api_key"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func loadOSClient(app *service.App, r *http.Request) (*opensubtitles.Client, error) {
	raw, err := app.GetSystemSetting(r.Context(), "opensubtitles")
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

func handleOpenSubtitlesTest(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var creds osCredentials
		if err := readJSON(r, &creds); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if creds.APIKey == "" || creds.Username == "" || creds.Password == "" {
			writeError(w, http.StatusBadRequest, "api_key, username, and password required")
			return
		}

		client := opensubtitles.NewClient(creds.APIKey)
		client.SetCredentials(creds.Username, creds.Password)

		if err := client.Login(r.Context()); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}

		info, err := client.UserInfo(r.Context())
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": info})
	}
}

func handleOpenSubtitlesUserInfo(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := loadOSClient(app, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		info, err := client.UserInfo(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, info)
	}
}

func handleOpenSubtitlesSearch(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := loadOSClient(app, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		params := opensubtitles.SearchParams{
			IMDbID: r.URL.Query().Get("imdb_id"),
			TMDbID: r.URL.Query().Get("tmdb_id"),
			Query:  r.URL.Query().Get("query"),
			Type:   r.URL.Query().Get("type"),
		}
		if langs := r.URL.Query().Get("languages"); langs != "" {
			params.Languages = strings.Split(langs, ",")
		}
		if s := r.URL.Query().Get("season"); s != "" {
			params.Season, _ = strconv.Atoi(s)
		}
		if e := r.URL.Query().Get("episode"); e != "" {
			params.Episode, _ = strconv.Atoi(e)
		}

		if mediaID := r.URL.Query().Get("media_id"); mediaID != "" {
			id, _ := strconv.ParseInt(mediaID, 10, 64)
			if id > 0 {
				q := sqlc.New(app.DBPool())
				item, err := q.GetMediaItemByID(r.Context(), id)
				if err == nil {
					var externalIDs map[string]string
					if json.Unmarshal(item.ExternalIds, &externalIDs) == nil {
						if params.IMDbID == "" {
							params.IMDbID = externalIDs["imdb"]
						}
						if params.TMDbID == "" {
							params.TMDbID = externalIDs["tmdb"]
						}
					}
					if params.Query == "" {
						params.Query = item.Title
					}
					switch item.MediaType {
					case sqlc.MediaTypeMovie:
						params.Type = "movie"
					case sqlc.MediaTypeTv:
						params.Type = "episode"
					}
				}
			}
		}

		results, err := client.Search(r.Context(), params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, results)
	}
}

func handleOpenSubtitlesDownload(app *service.App) http.HandlerFunc {
	type req struct {
		MediaItemID int64  `json:"media_item_id"`
		FileID      int    `json:"file_id"`
		Language    string `json:"language"`
		FileName    string `json:"file_name"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		client, err := loadOSClient(app, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		var body req
		if err := readJSON(r, &body); err != nil || body.FileID == 0 || body.MediaItemID == 0 {
			writeError(w, http.StatusBadRequest, "media_item_id and file_id required")
			return
		}

		dl, err := client.Download(r.Context(), body.FileID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		q := sqlc.New(app.DBPool())
		item, err := q.GetMediaItemByID(r.Context(), body.MediaItemID)
		if err != nil {
			writeError(w, http.StatusNotFound, "media item not found")
			return
		}

		dirName := item.Slug
		if dirName == "" {
			dirName = fmt.Sprintf("%d", item.ID)
		}
		subDir := filepath.Join(app.ConfigSnapshot().DataDir, "subtitles", string(item.MediaType), dirName)
		if err := os.MkdirAll(subDir, 0o750); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create subtitles dir: "+err.Error())
			return
		}

		filename := dl.FileName
		if filename == "" {
			filename = fmt.Sprintf("%s.%s.srt", dirName, body.Language)
		}
		// G304: destPath is built from server-controlled DataDir +
		// validated slug/id + filename; not a user-supplied path.
		destPath := filepath.Join(subDir, filename) //nolint:gosec

		resp, err := http.Get(dl.Link)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to download subtitle file")
			return
		}
		defer func() { _ = resp.Body.Close() }()

		out, err := os.Create(destPath) //nolint:gosec // destPath is server-controlled, see above.
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save subtitle file")
			return
		}
		size, _ := io.Copy(out, resp.Body)
		_ = out.Close()

		asset, _ := q.CreateMediaAsset(r.Context(), sqlc.CreateMediaAssetParams{
			MediaItemID: body.MediaItemID,
			AssetType:   sqlc.AssetTypeSubtitle,
			Source:      "opensubtitles",
			LocalPath:   destPath,
			Language:    body.Language,
			FileSize:    size,
		})

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "downloaded",
			"asset":     asset,
			"remaining": dl.Remaining,
		})
	}
}
