package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/rs/zerolog/log"
)

// registerMediaRoutes covers the broad /api/media + discovery surface:
// listings, search, dashboard stats, recommendations, activity feed,
// collections/genres/people/studios facets, transcoder settings, filesystem
// browse. Streaming endpoints live in stream_huma.go.
func registerMediaRoutes(api huma.API, app *service.App) {
	// --- Media listings ---
	huma.Register(api, secured(op(http.MethodGet, "/api/media", "list-media", "Paginated media items by type", "Media")),
		func(ctx context.Context, in *struct {
			Type string `query:"type" enum:"movie,tv,music,book,comic,podcast,radio" example:"movie" doc:"Media type bucket"`
			Sort string `query:"sort" enum:"title,added" default:"title" doc:"title = alphabetical, added = newest first"`
			Pagination
		}) (*JSONOutput[[]service.MediaItemView], error) {
			if in.Type == "" {
				return nil, huma.Error400BadRequest("?type= parameter is required")
			}
			list := app.ListMedia
			if in.Sort == "added" {
				list = app.ListMediaRecent
			}
			views, err := list(ctx, sqlc.MediaType(in.Type), in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(views, 30), nil
		})

	// The TV rail groups file arrivals Plex-style (new series / new season /
	// new episodes) instead of listing bare shows — a static path segment, so
	// it must register before /api/media/{id} can swallow it as a slug.
	huma.Register(api, secured(op(http.MethodGet, "/api/media/tv/recently-added", "recently-added-tv", "Grouped recently-added TV entries", "Media")),
		func(ctx context.Context, in *struct {
			Limit  int32 `query:"limit" minimum:"1" maximum:"100" default:"20"`
			Offset int32 `query:"offset" minimum:"0" default:"0" doc:"Entry offset — deeper pages regroup the full arrival history"`
		}) (*JSONOutput[[]service.RecentlyAddedTVEntry], error) {
			entries, err := app.ListRecentlyAddedTV(ctx, in.Limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(entries, 30), nil
		})

	// Ambient-background candidates — random artwork-bearing items for the
	// rotating page background. Static path segment, so it registers before
	// /api/media/{id} can swallow it as a slug.
	huma.Register(api, secured(op(http.MethodGet, "/api/media/ambient-backdrops", "ambient-backdrops", "Random media items with artwork for the ambient background", "Media")),
		func(ctx context.Context, in *struct {
			Types string `query:"types" example:"movie,tv,anime" doc:"Comma-separated media types (movie,tv,anime,music,book). Empty = all five."`
			Limit int32  `query:"limit" minimum:"1" maximum:"100" default:"30"`
		}) (*JSONOutput[[]service.AmbientBackdropItem], error) {
			allowed := map[string]bool{"movie": true, "tv": true, "anime": true, "music": true, "book": true}
			var types []string
			for _, t := range strings.Split(in.Types, ",") {
				if t = strings.TrimSpace(t); allowed[t] {
					types = append(types, t)
				}
			}
			if len(types) == 0 {
				types = []string{"movie", "tv", "anime", "music", "book"}
			}
			items, err := app.SampleAmbientBackdrops(ctx, types, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(items, 60), nil
		})

	// /enriched returns the two enriched-view shapes under a discriminated
	// envelope so the spec can describe both bodies. The previous design
	// returned the raw slice with no way to tell movies from tv in OpenAPI.
	huma.Register(api, secured(op(http.MethodGet, "/api/media/enriched", "list-enriched-media", "Movie/TV listings with derived fields", "Media")),
		func(ctx context.Context, in *struct {
			Type   string `query:"type" doc:"movie or tv" enum:"movie,tv"`
			Limit  int32  `query:"limit" minimum:"1" maximum:"5000" default:"2000"`
			Offset int32  `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[enrichedMediaBody], error) {
			body := enrichedMediaBody{Type: in.Type}
			switch in.Type {
			case "movie":
				views, err := app.ListEnrichedMovies(ctx, in.Limit, in.Offset)
				if err != nil {
					return nil, huma.Error500InternalServerError(err.Error())
				}
				body.Movies = views
			case "tv":
				views, err := app.ListEnrichedTVSeries(ctx, in.Limit, in.Offset)
				if err != nil {
					return nil, huma.Error500InternalServerError(err.Error())
				}
				body.TV = views
			default:
				return nil, huma.Error400BadRequest("?type=movie or ?type=tv is required")
			}
			return cachedJSON(body, 30), nil
		})

	// GetMediaDetail still returns map[string]any (legacy ad-hoc detail blob).
	// Typing the surface as a loose object is honest about that without
	// committing to a schema we'd have to keep in sync — schema tightening is
	// future work once the detail shape settles.
	huma.Register(api, secured(op(http.MethodGet, "/api/media/{id}", "get-media", "Media item detail (numeric ID or slug)", "Media")),
		func(ctx context.Context, in *SlugOrIDPath) (*JSONOutput[map[string]any], error) {
			result, err := app.GetMediaDetail(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("media item not found")
			}
			return cachedJSON(result, 30), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/media/{id}/refresh", "refresh-media", "Force a metadata refresh for one item", "Media")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if err := app.RefreshMediaItem(ctx, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("refreshed"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/media/missing", "list-missing", "Media whose files no longer exist", "Media")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.MissingMediaItem], error) {
			items, err := app.ListMissingMedia(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("query failed")
			}
			return cachedJSON(items, 30), nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/media/missing", "cleanup-missing", "Delete missing media records", "Media")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[deletedCountBody], error) {
			count, err := app.CleanupMissingMedia(ctx)
			if err != nil {
				log.Error().Err(err).Msg("cleanup missing media failed")
				return nil, huma.Error500InternalServerError("failed to clean up missing items")
			}
			return &JSONOutput[deletedCountBody]{Body: deletedCountBody{Deleted: count}}, nil
		})

	// --- People ---
	// /api/person/{id} stays singular for now — moves to /api/people/{id} in
	// the URL-consolidation pass so FE callers update in one batch.
	huma.Register(api, secured(op(http.MethodGet, "/api/person/{id}", "get-person", "Person detail (numeric ID or slug)", "People")),
		func(ctx context.Context, in *SlugOrIDPath) (*JSONOutput[map[string]any], error) {
			result, err := app.GetPerson(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("person not found")
			}
			return cachedJSON(result, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/people/search", "search-people", "Typeahead search across people", "People")),
		func(ctx context.Context, in *struct {
			Q     string `query:"q" maxLength:"200" example:"miyazaki" doc:"Query string"`
			Limit int32  `query:"limit" minimum:"1" maximum:"50" default:"10"`
		}) (*JSONOutput[[]sqlc.SearchPeopleByNameRow], error) {
			if in.Q == "" {
				return cachedJSON([]sqlc.SearchPeopleByNameRow{}, 30), nil
			}
			results, err := app.SearchPeople(ctx, in.Q, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(results, 30), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/people/media-ids", "people-media-ids", "Resolve media IDs starring listed people", "People")),
		func(ctx context.Context, in *struct {
			Body struct {
				PersonIDs []int64 `json:"person_ids"`
			}
		}) (*JSONOutput[[]int64], error) {
			result, err := app.ListMediaIDsByPeople(ctx, in.Body.PersonIDs)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[[]int64]{Body: result}, nil
		})

	// --- Studios ---
	huma.Register(api, secured(op(http.MethodGet, "/api/studios/search", "search-studios", "Typeahead search across studios", "Studios")),
		func(ctx context.Context, in *struct {
			Q     string `query:"q" maxLength:"200" example:"a24"`
			Limit int32  `query:"limit" minimum:"1" maximum:"50" default:"10"`
		}) (*JSONOutput[[]sqlc.SearchProductionCompaniesByNameRow], error) {
			if in.Q == "" {
				return cachedJSON([]sqlc.SearchProductionCompaniesByNameRow{}, 30), nil
			}
			results, err := app.SearchStudios(ctx, in.Q, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(results, 30), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/studios/media-ids", "studios-media-ids", "Resolve media IDs produced by listed studios", "Studios")),
		func(ctx context.Context, in *struct {
			Body struct {
				CompanyIDs []int64 `json:"company_ids"`
			}
		}) (*JSONOutput[[]int64], error) {
			ids, err := app.ListMediaIDsByStudio(ctx, in.Body.CompanyIDs)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[[]int64]{Body: ids}, nil
		})

	// --- Search ---
	huma.Register(api, secured(op(http.MethodGet, "/api/search", "search-all", "Full-text search across all media", "Search")),
		func(ctx context.Context, in *struct {
			Q    string `query:"q" minLength:"1" maxLength:"200" example:"godfather"`
			Type string `query:"type" enum:"movie,tv,music,book,comic,podcast,radio,episode,person,albums,tracks" example:"movie" doc:"Optional bucket"`
			Pagination
		}) (*JSONOutput[service.SearchBucket], error) {
			q := strings.TrimSpace(in.Q)
			if q == "" {
				return nil, huma.Error400BadRequest("?q= parameter is required")
			}
			limit := in.Limit
			if limit <= 0 || limit > 200 {
				limit = 60
			}
			result, err := app.SearchByType(ctx, q, in.Type, limit, in.Offset)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return cachedJSON(result, 30), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/search/quick", "search-quick", "Lightweight cross-bucket search for the omni-search popover", "Search")),
		func(ctx context.Context, in *struct {
			Q string `query:"q" maxLength:"200" example:"blade runner"`
		}) (*JSONOutput[service.QuickSearchResult], error) {
			result, err := app.SearchQuick(ctx, strings.TrimSpace(in.Q))
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(result, 30), nil
		})

	// --- Genres / keywords / collections ---
	huma.Register(api, secured(op(http.MethodGet, "/api/genres", "list-genres", "All genres with counts", "Discover")),
		simpleGet(app.ListGenres, 60))

	huma.Register(api, secured(op(http.MethodGet, "/api/genres/{name}", "get-genre", "Media within a genre", "Discover")),
		func(ctx context.Context, in *struct {
			Name string `path:"name" maxLength:"128" example:"Sci-Fi & Fantasy" doc:"Exact genre/keyword name, URL-encoded; matched verbatim (dashes are literal, not space separators)"`
			Type string `query:"type" enum:"movie,tv,anime,book,music,comic" doc:"Restrict to one media type; empty = all"`
			Sort string `query:"sort" enum:"title,year-desc,year-asc" default:"title" doc:"Server-side sort — the browse grid is random-access paged"`
			Pagination
		}) (*JSONOutput[service.GenreResult], error) {
			limit := in.Limit
			if limit <= 0 || limit > 200 {
				limit = 60
			}
			result, err := app.GetGenre(ctx, in.Name, in.Type, in.Sort, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(result, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/keywords/{name}", "get-keyword", "Media tagged with a keyword", "Discover")),
		func(ctx context.Context, in *struct {
			Name string `path:"name" maxLength:"128" example:"Sci-Fi & Fantasy" doc:"Exact genre/keyword name, URL-encoded; matched verbatim (dashes are literal, not space separators)"`
			Type string `query:"type" enum:"movie,tv,anime,book,music,comic" doc:"Restrict to one media type; empty = all"`
			Sort string `query:"sort" enum:"title,year-desc,year-asc" default:"title" doc:"Server-side sort — the browse grid is random-access paged"`
			Pagination
		}) (*JSONOutput[service.KeywordResult], error) {
			limit := in.Limit
			if limit <= 0 || limit > 200 {
				limit = 60
			}
			result, err := app.GetKeyword(ctx, in.Name, in.Type, in.Sort, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(result, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/collections", "list-collections", "All collections", "Discover")),
		func(ctx context.Context, in *Pagination) (*JSONOutput[service.CollectionListResult], error) {
			limit := in.Limit
			if limit <= 0 || limit > 200 {
				limit = 60
			}
			result, err := app.ListCollections(ctx, limit, in.Offset)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return cachedJSON(result, 60), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/collections/browse", "browse-collections", "Browseable collection listing", "Discover")),
		simpleGet(app.BrowseCollections, 60))

	huma.Register(api, secured(op(http.MethodGet, "/api/collections/{id}", "get-collection", "Collection detail", "Discover")),
		func(ctx context.Context, in *IDPath) (*JSONOutput[service.CollectionResult], error) {
			result, err := app.GetCollection(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("collection not found")
			}
			return cachedJSON(result, 60), nil
		})

	// --- Recommendations / activity / stats ---
	huma.Register(api, secured(op(http.MethodGet, "/api/recommendations", "list-recommendations", "Aggregated recommendation feed", "Discover")),
		func(ctx context.Context, in *struct {
			Limit int32 `query:"limit" minimum:"1" maximum:"50" default:"20"`
		}) (*JSONOutput[[]recItem], error) {
			recs, err := app.ListTopRecommendations(ctx, in.Limit)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			items := make([]recItem, len(recs))
			for i, r := range recs {
				var extIDs map[string]string
				_ = json.Unmarshal(r.ExternalIds, &extIDs)
				items[i] = recItem{
					ExternalIDs:   extIDs,
					Title:         r.Title,
					PosterPath:    r.PosterPath,
					MediaType:     r.MediaType,
					VoteAverage:   r.VoteAverage,
					ProviderScore: r.ProviderScore,
					ReleaseDate:   r.ReleaseDate,
					SourceCount:   r.SourceCount,
				}
				if r.LocalMediaItemID != 0 {
					items[i].LocalMediaID = &r.LocalMediaItemID
				}
				if r.LocalSlug != "" {
					items[i].LocalSlug = &r.LocalSlug
				}
				if r.LocalPosterPath != "" {
					items[i].LocalPosterPath = &r.LocalPosterPath
				}
			}
			return cachedJSON(items, 120), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/activity", "activity-feed", "Recent activity events", "Discover")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]service.ActivityItem], error) {
			return noStoreJSON(app.GetActivityFeed(ctx)), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/stats", "dashboard-stats", "Dashboard counts", "System")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.DashboardStats], error) {
			return noStoreJSON(app.GetDashboardStats(ctx)), nil
		})

	// --- Filesystem browser ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/fs/browse", "fs-browse", "Filesystem directory listing (library wizard)", "System")),
		func(ctx context.Context, in *struct {
			Path string `query:"path" doc:"Absolute directory to list"`
		}) (*JSONOutput[fsBrowseBody], error) {
			dir := in.Path
			if dir == "" {
				if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
					dir = "/"
				} else {
					dir = "C:\\"
				}
			}
			dir = filepath.Clean(dir)
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				return nil, huma.Error400BadRequest("path is not a valid directory")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return nil, huma.Error403Forbidden("cannot read directory")
			}
			var dirs []fsEntry
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".") || !e.IsDir() {
					continue
				}
				dirs = append(dirs, fsEntry{
					Name:  e.Name(),
					Path:  filepath.Join(dir, e.Name()),
					IsDir: true,
				})
			}
			sort.Slice(dirs, func(i, j int) bool {
				return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
			})
			body := fsBrowseBody{Path: dir, Entries: dirs}
			if dir != "/" {
				body.Parent = filepath.Dir(dir)
			}
			return noStoreJSON(body), nil
		})

	// --- Transcoding settings (status + reconfig + cache clear) ---
	huma.Register(api, secured(op(http.MethodGet, "/api/transcode/status", "transcode-status", "Transcoder runtime status", "Transcoding")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[transcodeStatusBody], error) {
			resp := transcodeStatusBody{
				Available:  transcoder.IsFFmpegAvailable(),
				ConfigMode: app.ConfigSnapshot().HWAccel.Value,
				CacheDir:   app.ConfigSnapshot().TranscodeCacheDir.Value,
				CacheMaxGB: app.ConfigSnapshot().TranscodeCacheMaxGB.Value,
			}
			if app.TranscoderSessions() != nil {
				hw := app.TranscoderSessions().HWAccel()
				resp.HWAccel = string(hw.Type)
				resp.HWAccelLabel = hwAccelLabel(hw.Type)
				resp.EncoderH264 = hw.EncoderH264
				resp.EncoderHEVC = hw.EncoderHEVC
			} else {
				resp.HWAccel = "none"
				resp.HWAccelLabel = "Disabled"
			}
			if app.TranscoderCache() != nil {
				stats := app.TranscoderCache().Stats()
				resp.CacheSizeMB = stats.TotalSize / (1024 * 1024)
				resp.CacheItems = stats.ItemCount
			}
			if app.TranscoderSessions() != nil {
				resp.ActiveJobs = len(app.TranscoderSessions().Overview())
			}
			return noStoreJSON(resp), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/transcode/sessions", "transcode-sessions", "Live transcode sessions", "Transcoding")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[transcodeSessionsBody], error) {
			body := transcodeSessionsBody{Sessions: []transcodeSessionBody{}}
			if app.TranscoderSessions() == nil {
				return noStoreJSON(body), nil
			}
			for _, ov := range app.TranscoderSessions().Overview() {
				running, state := transcodeSessionState(ov.Head, ov.Progress)
				sess := transcodeSessionBody{
					Key:                  ov.Key,
					File:                 filepath.Base(ov.FilePath),
					Path:                 ov.FilePath,
					Container:            ov.Container,
					VideoCodec:           ov.VideoCodec,
					AudioCodec:           ov.AudioCodec,
					Quality:              ov.Quality,
					Running:              running,
					State:                state,
					DurationSeconds:      ov.Duration,
					TotalSegments:        ov.TotalSegs,
					ReadySegments:        ov.ReadySegs,
					HeadStartSegment:     ov.Head.StartSeg,
					HeadCurrentSegment:   ov.Head.CurrentSeg,
					LastRequestedSegment: ov.LastRequestedSeg,
					EncoderPosSeconds:    ov.EncoderPos,
					PlayerPosSeconds:     ov.PlayerPos,
					IdleSeconds:          ov.IdleSeconds,
					FPS:                  ov.Progress.FPS,
					Speed:                ov.Progress.Speed,
					BitrateKbps:          ov.Progress.Bitrate,
				}
				body.Sessions = append(body.Sessions, sess)
			}
			return noStoreJSON(body), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/transcode/settings", "update-transcode-settings", "Update transcoder settings", "Transcoding")),
		func(ctx context.Context, in *struct {
			Body struct {
				HWAccel    string `json:"hw_accel" enum:"auto,none,vaapi,qsv,nvenc,videotoolbox"`
				CacheMaxGB int    `json:"cache_max_gb" minimum:"0"`
			}
		}) (*StatusOutput, error) {
			if err := app.SaveTranscoderSettings(ctx, in.Body.HWAccel, in.Body.CacheMaxGB); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("ok"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/transcode/cache", "clear-transcode-cache", "Clear cached transcoded segments", "Transcoding")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			if app.TranscoderCache() == nil {
				return nil, huma.Error503ServiceUnavailable("transcoding not available")
			}
			if err := app.TranscoderCache().Clear(); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("cleared"), nil
		})
}

type deletedCountBody struct {
	Deleted int `json:"deleted"`
}

// enrichedMediaBody is the discriminated response for /api/media/enriched.
// `type` tells callers which of `movies`/`tv` is populated — the other is
// omitted from the response. Spec-wise this is much friendlier than the old
// "raw slice of either shape" return.
type enrichedMediaBody struct {
	Type   string                      `json:"type" doc:"Echoes the requested ?type=" enum:"movie,tv"`
	Movies []service.EnrichedMovieView `json:"movies,omitempty"`
	TV     []service.EnrichedTVView    `json:"tv,omitempty"`
}

type recItem struct {
	ExternalIDs     map[string]string `json:"external_ids"`
	Title           string            `json:"title"`
	PosterPath      string            `json:"poster_path"`
	MediaType       string            `json:"media_type"`
	VoteAverage     any               `json:"vote_average"`
	ProviderScore   float64           `json:"provider_score,omitempty"`
	ReleaseDate     string            `json:"release_date"`
	LocalMediaID    *int64            `json:"local_media_item_id,omitempty"`
	LocalSlug       *string           `json:"local_slug,omitempty"`
	LocalPosterPath *string           `json:"local_poster_path,omitempty"`
	SourceCount     int32             `json:"source_count"`
}

type fsEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

type fsBrowseBody struct {
	Path    string    `json:"path"`
	Parent  string    `json:"parent,omitempty"`
	Entries []fsEntry `json:"entries"`
}

type transcodeSessionsBody struct {
	Sessions []transcodeSessionBody `json:"sessions"`
}

type transcodeSessionBody struct {
	Key                  string  `json:"key"`
	File                 string  `json:"file"`
	Path                 string  `json:"path"`
	Container            string  `json:"container"`
	VideoCodec           string  `json:"video_codec"`
	AudioCodec           string  `json:"audio_codec"`
	Quality              string  `json:"quality"`
	Running              bool    `json:"running"`
	State                string  `json:"state"`
	DurationSeconds      float64 `json:"duration_seconds"`
	TotalSegments        int     `json:"total_segments"`
	ReadySegments        int     `json:"ready_segments"`
	HeadStartSegment     int     `json:"head_start_segment"`
	HeadCurrentSegment   int     `json:"head_current_segment"`
	LastRequestedSegment int     `json:"last_requested_segment"`
	EncoderPosSeconds    float64 `json:"encoder_pos_seconds"`
	PlayerPosSeconds     float64 `json:"player_pos_seconds"`
	IdleSeconds          float64 `json:"idle_seconds"`
	FPS                  float64 `json:"fps"`
	Speed                float64 `json:"speed"`
	BitrateKbps          float64 `json:"bitrate_kbps"`
}

// transcodeSessionState collapses head/progress snapshots into the state
// vocabulary shared by the per-file telemetry endpoint and the admin
// sessions list.
func transcodeSessionState(head transcoder.HeadInfo, stats transcoder.ProgressStats) (bool, string) {
	running := stats.Running || head.Running
	switch {
	case running:
		return true, "running"
	case head.StopReason == transcoder.StopReasonLeadCap:
		return false, "throttled"
	case head.StopReason == transcoder.StopReasonCompleted:
		return false, "completed"
	case head.StopReason == transcoder.StopReasonKilled:
		return false, "killed"
	case head.StopReason == transcoder.StopReasonExited:
		return false, "exited"
	}
	return false, "idle"
}

type transcodeStatusBody struct {
	Available    bool   `json:"available"`
	HWAccel      string `json:"hw_accel"`
	HWAccelLabel string `json:"hw_accel_label"`
	EncoderH264  string `json:"encoder_h264"`
	EncoderHEVC  string `json:"encoder_hevc"`
	CacheDir     string `json:"cache_dir"`
	CacheMaxGB   int    `json:"cache_max_gb"`
	CacheSizeMB  int64  `json:"cache_size_mb"`
	CacheItems   int    `json:"cache_items"`
	ActiveJobs   int    `json:"active_jobs"`
	ConfigMode   string `json:"config_mode"`
}

// Re-exported so handlers in other huma files (sonic analysis, etc.) can refer
// to App's getter without dragging in transcoder. Kept here because the
// transcode status body needs the same labels.
func hwAccelLabel(t transcoder.HwAccelType) string {
	switch t {
	case transcoder.HwAccelNVENC:
		return "NVIDIA NVENC"
	case transcoder.HwAccelVAAPI:
		return "VA-API"
	case transcoder.HwAccelQSV:
		return "Intel Quick Sync"
	case transcoder.HwAccelVideoToolbox:
		return "Apple VideoToolbox"
	case transcoder.HwAccelNone:
		return "CPU (Software)"
	default:
		return "Unknown"
	}
}
