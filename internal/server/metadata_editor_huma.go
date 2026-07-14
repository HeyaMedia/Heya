package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/service"
)

// registerMetadataEditorRoutes covers the manual fix-up surface used by the
// metadata-editor dialog: library media listings, free-form metadata edits,
// per-episode edits, provider identify, asset CRUD, and stream/file inspection.
func registerMetadataEditorRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/libraries/{id}/media", "list-library-media", "Lightweight media listing for the editor", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Q      string `query:"q" maxLength:"200" doc:"Optional title filter"`
			Limit  int32  `query:"limit" minimum:"1" maximum:"5000" default:"500"`
			Offset int32  `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[[]sqlc.MediaItemCard], error) {
			items, err := app.ListLibraryMedia(ctx, in.ID, in.Limit, in.Offset, in.Q)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(items, 30), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/media/{id}/metadata", "update-media-metadata", "Edit media metadata fields", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Body service.UpdateMediaMetadataReq
		}) (*StatusOutput, error) {
			if err := app.UpdateMediaMetadata(ctx, in.ID, in.Body); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("updated"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/media/{id}/episode/{episode_id}", "update-episode", "Edit a single episode", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			EpisodeID int64 `path:"episode_id" minimum:"1"`
			Body      service.UpdateEpisodeReq
		}) (*JSONOutput[sqlc.TvEpisode], error) {
			updated, err := app.UpdateEpisode(ctx, in.EpisodeID, in.Body)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[sqlc.TvEpisode]{Body: updated}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/media/{id}/season/{season_id}", "update-season", "Edit a TV season", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			SeasonID int64 `path:"season_id" minimum:"1"`
			Body     service.UpdateSeasonReq
		}) (*JSONOutput[sqlc.TvSeason], error) {
			updated, err := app.UpdateSeason(ctx, in.SeasonID, in.Body)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[sqlc.TvSeason]{Body: updated}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/media/{id}/identify", "identify-search", "Provider search for re-identification", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Q    string `query:"q" maxLength:"200" doc:"Title query"`
			Year string `query:"year" maxLength:"4" pattern:"^[0-9]*$" doc:"Year hint (4-digit)"`
		}) (*JSONOutput[identifyBody], error) {
			result, err := app.IdentifySearch(ctx, in.ID, in.Q, in.Year, metadata.MediaKind(""))
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[identifyBody]{Body: identifyBody{Results: result.Results}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/media/{id}/identify", "apply-identify", "Switch the media item to a chosen provider match", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				ProviderName string `json:"provider_name" minLength:"1" maxLength:"32"`
				ProviderID   string `json:"provider_id" minLength:"1" maxLength:"256"`
			}
		}) (*StatusOutput, error) {
			if err := app.ApplyIdentify(ctx, in.ID, in.Body.ProviderName, in.Body.ProviderID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("identified"), nil
		})

	// --- Asset CRUD ---
	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/media/{id}/assets/{asset_id}", "delete-asset", "Delete a media asset", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			AssetID int64 `path:"asset_id" minimum:"1"`
		}) (*StatusOutput, error) {
			if err := app.DeleteMediaAsset(ctx, in.ID, in.AssetID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("deleted"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/media/{id}/assets/{asset_id}/primary", "set-primary-asset", "Pin an asset as primary for its type", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			AssetID int64 `path:"asset_id" minimum:"1"`
		}) (*StatusOutput, error) {
			if err := app.SetPrimaryAsset(ctx, in.ID, in.AssetID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("updated"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/media/{id}/assets/search", "search-provider-artwork", "Search upstream provider artwork", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Type     string `query:"type" enum:",poster,backdrop,logo,art,clearart,banner,thumb,disc,still" doc:"Filter by asset type (empty = all)"`
			Provider string `query:"provider" maxLength:"32" doc:"Filter by provider name"`
		}) (*JSONOutput[artworkBody], error) {
			results, err := app.SearchProviderArtwork(ctx, in.ID, in.Type, in.Provider)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[artworkBody]{Body: artworkBody{Results: results}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/media/{id}/assets/download", "download-asset", "Queue an artwork download from a URL", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			Body struct {
				URL       string `json:"url" minLength:"1" maxLength:"2048" format:"uri"`
				AssetType string `json:"asset_type" enum:"poster,backdrop,logo,art,clearart,banner,thumb,disc,still"`
				Label     string `json:"label,omitempty" maxLength:"128"`
			}
		}) (*StatusOutput, error) {
			if err := app.DownloadAsset(ctx, in.ID, in.Body.URL, in.Body.AssetType, in.Body.Label); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("queued"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/media/{id}/files", "media-files", "Per-file ffprobe stream summary", "Metadata Editor")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[[]mediaFileInfo], error) {
			q := sqlc.New(app.DBPool())
			files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: in.ID, Valid: true})
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to list files")
			}
			result := make([]mediaFileInfo, 0, len(files))
			for _, f := range files {
				result = append(result, buildMediaFileInfo(f))
			}
			return &JSONOutput[[]mediaFileInfo]{Body: result}, nil
		})

	// Upload artwork via multipart/form-data. Huma decodes the `file` field
	// into a typed FormFile; the asset_type string field is read off the raw
	// form (Huma's MultipartFormFiles only auto-binds FormFile/[]FormFile).
	huma.Register(api, adminSecured(op(http.MethodPost, "/api/media/{id}/assets/upload", "upload-media-asset", "Upload an artwork file", "Metadata Editor")),
		func(ctx context.Context, in *struct {
			IDPath
			RawBody huma.MultipartFormFiles[uploadAssetForm]
		}) (*JSONOutput[uploadAssetResultBody], error) {
			data := in.RawBody.Data()
			if !data.File.IsSet {
				return nil, huma.Error400BadRequest("file field required")
			}
			defer func() { _ = data.File.Close() }()

			assetType := "poster"
			if vs := in.RawBody.Form.Value["asset_type"]; len(vs) > 0 && vs[0] != "" {
				assetType = vs[0]
			}
			label := ""
			if vs := in.RawBody.Form.Value["label"]; len(vs) > 0 {
				label = vs[0]
			}

			result, err := app.UploadMediaAsset(ctx, in.ID, data.File, data.File.Filename, assetType, label)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			body := uploadAssetResultBody{Status: "uploaded", Path: result.Path}
			if result.Asset != nil {
				body.Asset = result.Asset
			}
			return &JSONOutput[uploadAssetResultBody]{Body: body}, nil
		})
}

// uploadAssetForm declares the multipart/form-data schema for asset uploads.
// `file` is auto-bound by Huma; `asset_type` is read from the raw form values
// since Huma doesn't auto-bind plain string form fields.
type uploadAssetForm struct {
	File huma.FormFile `form:"file" contentType:"image/*" required:"true"`
	// asset_type is declared here only so it shows up in the OpenAPI schema;
	// the actual value is read from RawBody.Form.Value["asset_type"].
	AssetType string `form:"asset_type" doc:"poster|backdrop|logo|… (defaults to poster)"`
	Label     string `form:"label" doc:"Optional season/episode asset label"`
}

type identifyBody struct {
	Results any `json:"results"`
}

type artworkBody struct {
	Results any `json:"results"`
}

// uploadAssetResultBody covers both legs of UploadMediaAsset's two-shape
// return: when the asset was created in DB we surface it; otherwise we just
// confirm the file was written and where. Asset is omitted when nil.
type uploadAssetResultBody struct {
	Status string           `json:"status"`
	Path   string           `json:"path,omitempty"`
	Asset  *sqlc.MediaAsset `json:"asset,omitempty"`
}

type mediaFileStream struct {
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

type mediaFileInfo struct {
	ID        int64             `json:"id"`
	Path      string            `json:"path"`
	Filename  string            `json:"filename"`
	Size      int64             `json:"size"`
	Container string            `json:"container,omitempty"`
	Duration  float64           `json:"duration,omitempty"`
	BitRate   int64             `json:"bit_rate,omitempty"`
	Streams   []mediaFileStream `json:"streams,omitempty"`
}

func buildMediaFileInfo(f sqlc.LibraryFile) mediaFileInfo {
	fi := mediaFileInfo{
		ID:       f.ID,
		Path:     f.Path,
		Filename: filepath.Base(f.Path),
		Size:     f.Size,
	}
	if len(f.MediaInfo) <= 2 {
		return fi
	}
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
	if json.Unmarshal(f.MediaInfo, &mi) != nil {
		return fi
	}
	fi.Container = mi.Container
	fi.Duration = mi.Duration
	fi.BitRate = mi.BitRate
	for _, s := range mi.Streams {
		fi.Streams = append(fi.Streams, mediaFileStream{
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
		})
	}
	return fi
}
