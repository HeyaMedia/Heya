package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/service"
)

func handleListLibraryMedia(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		limit := int32(500)
		offset := int32(0)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, e := strconv.ParseInt(l, 10, 32); e == nil {
				limit = int32(n)
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if n, e := strconv.ParseInt(o, 10, 32); e == nil {
				offset = int32(n)
			}
		}
		q := r.URL.Query().Get("q")

		items, err := app.ListLibraryMedia(r.Context(), id, limit, offset, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleUpdateMediaMetadata(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		var req service.UpdateMediaMetadataReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.UpdateMediaMetadata(r.Context(), id, req); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

func handleUpdateEpisode(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		epID, err := strconv.ParseInt(r.PathValue("episode_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid episode id")
			return
		}

		var req service.UpdateEpisodeReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		updated, err := app.UpdateEpisode(r.Context(), epID, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

func handleIdentifySearch(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		query := r.URL.Query().Get("q")
		year := r.URL.Query().Get("year")

		result, err := app.IdentifySearch(r.Context(), id, query, year, metadata.MediaKind(""))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"results": result.Results})
	}
}

func handleApplyIdentify(app *service.App) http.HandlerFunc {
	type req struct {
		ProviderName string `json:"provider_name"`
		ProviderID   string `json:"provider_id"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		var body req
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.ApplyIdentify(r.Context(), id, body.ProviderName, body.ProviderID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"status": "identified"})
	}
}

func handleDeleteMediaAsset(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}
		assetID, err := strconv.ParseInt(r.PathValue("asset_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid asset id")
			return
		}

		if err := app.DeleteMediaAsset(r.Context(), mediaID, assetID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func handleSetPrimaryAsset(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}
		assetID, err := strconv.ParseInt(r.PathValue("asset_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid asset id")
			return
		}

		if err := app.SetPrimaryAsset(r.Context(), mediaID, assetID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

func handleSearchProviderArtwork(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		filterType := r.URL.Query().Get("type")
		filterProvider := r.URL.Query().Get("provider")

		results, err := app.SearchProviderArtwork(r.Context(), id, filterType, filterProvider)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

func handleDownloadAsset(app *service.App) http.HandlerFunc {
	type req struct {
		URL       string `json:"url"`
		AssetType string `json:"asset_type"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}
		var body req
		if err := readJSON(r, &body); err != nil || body.URL == "" || body.AssetType == "" {
			writeError(w, http.StatusBadRequest, "url and asset_type are required")
			return
		}

		if err := app.DownloadAsset(r.Context(), id, body.URL, body.AssetType); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
	}
}

func handleUploadMediaAsset(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		if err := r.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form")
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "file field required")
			return
		}
		defer file.Close()

		assetType := r.FormValue("asset_type")
		if assetType == "" {
			assetType = "poster"
		}

		result, err := app.UploadMediaAsset(r.Context(), id, file, header.Filename, assetType)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if result.Asset != nil {
			writeJSON(w, http.StatusOK, result.Asset)
		} else {
			writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "path": result.Path})
		}
	}
}

func handleMediaFiles(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media ID")
			return
		}

		q := sqlc.New(app.DBPool())
		files, err := q.ListLibraryFilesByMediaItem(r.Context(), pgtype.Int8{Int64: id, Valid: true})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list files")
			return
		}

		type streamInfo struct {
			Index         int    `json:"index"`
			CodecName     string `json:"codec_name"`
			CodecType     string `json:"codec_type"`
			CodecLong     string `json:"codec_long_name,omitempty"`
			Language      string `json:"language,omitempty"`
			Title         string `json:"title,omitempty"`
			Width         int    `json:"width,omitempty"`
			Height        int    `json:"height,omitempty"`
			PixFmt        string `json:"pix_fmt,omitempty"`
			Profile       string `json:"profile,omitempty"`
			ColorSpace    string `json:"color_space,omitempty"`
			Channels      int    `json:"channels,omitempty"`
			ChannelLayout string `json:"channel_layout,omitempty"`
			SampleRate    string `json:"sample_rate,omitempty"`
			BitRate       string `json:"bit_rate,omitempty"`
			Default       bool   `json:"default"`
			Forced        bool   `json:"forced"`
		}

		type fileInfo struct {
			ID        int64        `json:"id"`
			Path      string       `json:"path"`
			Filename  string       `json:"filename"`
			Size      int64        `json:"size"`
			Container string       `json:"container,omitempty"`
			Duration  float64      `json:"duration,omitempty"`
			BitRate   int64        `json:"bit_rate,omitempty"`
			Streams   []streamInfo `json:"streams,omitempty"`
		}

		var result []fileInfo
		for _, f := range files {
			fi := fileInfo{
				ID:       f.ID,
				Path:     f.Path,
				Filename: filepath.Base(f.Path),
				Size:     f.Size,
			}

			if len(f.MediaInfo) > 2 {
				var mi struct {
					Container string  `json:"container"`
					Duration  float64 `json:"duration"`
					BitRate   int64   `json:"bit_rate"`
					Streams   []struct {
						Index         int    `json:"index"`
						CodecName     string `json:"codec_name"`
						CodecType     string `json:"codec_type"`
						CodecLong     string `json:"codec_long_name"`
						Width         int    `json:"width"`
						Height        int    `json:"height"`
						PixFmt        string `json:"pix_fmt"`
						Profile       string `json:"profile"`
						ColorSpace    string `json:"color_space"`
						Channels      int    `json:"channels"`
						ChannelLayout string `json:"channel_layout"`
						SampleRate    string `json:"sample_rate"`
						Tags          struct {
							Language string `json:"language"`
							Title    string `json:"title"`
							BPS      string `json:"BPS"`
						} `json:"tags"`
						Disposition struct {
							Default int `json:"default"`
							Forced  int `json:"forced"`
						} `json:"disposition"`
					} `json:"streams"`
				}
				if json.Unmarshal(f.MediaInfo, &mi) == nil {
					fi.Container = mi.Container
					fi.Duration = mi.Duration
					fi.BitRate = mi.BitRate
					for _, s := range mi.Streams {
						si := streamInfo{
							Index:         s.Index,
							CodecName:     s.CodecName,
							CodecType:     s.CodecType,
							CodecLong:     s.CodecLong,
							Language:      s.Tags.Language,
							Title:         s.Tags.Title,
							Width:         s.Width,
							Height:        s.Height,
							PixFmt:        s.PixFmt,
							Profile:       s.Profile,
							ColorSpace:    s.ColorSpace,
							Channels:      s.Channels,
							ChannelLayout: s.ChannelLayout,
							SampleRate:    s.SampleRate,
							BitRate:       s.Tags.BPS,
							Default:       s.Disposition.Default == 1,
							Forced:        s.Disposition.Forced == 1,
						}
						fi.Streams = append(fi.Streams, si)
					}
				}
			}

			result = append(result, fi)
		}

		writeJSON(w, http.StatusOK, result)
	}
}
