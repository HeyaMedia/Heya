package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

func registerRoutes(mux *http.ServeMux, app *service.App) {
	mux.HandleFunc("GET /api/health", healthHandler(app.DBPool()))
	mux.HandleFunc("GET /api/media/{id}/image/{type}", handleMediaImage(app))
	mux.HandleFunc("GET /api/person/{id}/image", handlePersonImage(app))
	mux.HandleFunc("GET /api/studio/{id}/image", handleStudioImage(app))

	mux.HandleFunc("POST /api/auth/register", handleRegister(app))
	mux.HandleFunc("POST /api/auth/login", handleLogin(app))
	mux.HandleFunc("POST /api/auth/logout", handleLogout(app))

	authed := auth.Middleware(app.SessionLookup())

	mux.Handle("GET /api/auth/me", authed(http.HandlerFunc(handleMe(app))))

	mux.Handle("POST /api/libraries", authed(http.HandlerFunc(handleCreateLibrary(app))))
	mux.Handle("GET /api/libraries", authed(http.HandlerFunc(handleListLibraries(app))))
	mux.Handle("GET /api/libraries/{id}", authed(http.HandlerFunc(handleGetLibrary(app))))
	mux.Handle("PUT /api/libraries/{id}", authed(http.HandlerFunc(handleUpdateLibrary(app))))
	mux.Handle("DELETE /api/libraries/{id}", authed(http.HandlerFunc(handleDeleteLibrary(app))))

	mux.Handle("PUT /api/libraries/{id}/settings", authed(http.HandlerFunc(handleUpdateLibrarySettings(app))))
	mux.Handle("GET /api/libraries/{id}/settings", authed(http.HandlerFunc(handleGetLibrarySettings(app))))
	mux.Handle("POST /api/libraries/{id}/scan", authed(http.HandlerFunc(handleScanLibrary(app))))
	mux.Handle("POST /api/libraries/{id}/scan/cancel", authed(http.HandlerFunc(handleCancelLibraryScan(app))))
	mux.Handle("POST /api/libraries/{id}/refresh-metadata", authed(http.HandlerFunc(handleForceRefreshMetadata(app))))
	mux.Handle("POST /api/libraries/{id}/refresh-images", authed(http.HandlerFunc(handleForceRefreshImages(app))))
	mux.Handle("POST /api/libraries/scan/cancel-all", authed(http.HandlerFunc(handleCancelAllScans(app))))
	mux.Handle("GET /api/libraries/{id}/files", authed(http.HandlerFunc(handleListLibraryFiles(app))))
	mux.Handle("GET /api/libraries/{id}/files/stats", authed(http.HandlerFunc(handleLibraryFileStats(app))))
	mux.Handle("GET /api/libraries/{id}/unmatched", authed(http.HandlerFunc(handleListUnmatched(app))))

	mux.Handle("POST /api/library-files/{id}/resolve", authed(http.HandlerFunc(handleResolveMatch(app))))

	// Metadata editor
	mux.Handle("GET /api/libraries/{id}/media", authed(http.HandlerFunc(handleListLibraryMedia(app))))
	mux.Handle("PUT /api/media/{id}/metadata", authed(http.HandlerFunc(handleUpdateMediaMetadata(app))))
	mux.Handle("PUT /api/media/{id}/episode/{episode_id}", authed(http.HandlerFunc(handleUpdateEpisode(app))))
	mux.Handle("GET /api/media/{id}/identify", authed(http.HandlerFunc(handleIdentifySearch(app))))
	mux.Handle("POST /api/media/{id}/identify", authed(http.HandlerFunc(handleApplyIdentify(app))))
	mux.Handle("DELETE /api/media/{id}/assets/{asset_id}", authed(http.HandlerFunc(handleDeleteMediaAsset(app))))
	mux.Handle("PUT /api/media/{id}/assets/{asset_id}/primary", authed(http.HandlerFunc(handleSetPrimaryAsset(app))))
	mux.Handle("GET /api/media/{id}/assets/search", authed(http.HandlerFunc(handleSearchProviderArtwork(app))))
	mux.Handle("POST /api/media/{id}/assets/download", authed(http.HandlerFunc(handleDownloadAsset(app))))
	mux.Handle("POST /api/media/{id}/assets/upload", authed(http.HandlerFunc(handleUploadMediaAsset(app))))
	mux.Handle("GET /api/media/{id}/files", authed(http.HandlerFunc(handleMediaFiles(app))))

	mux.Handle("GET /api/stats", authed(http.HandlerFunc(handleDashboardStats(app))))
	mux.Handle("GET /api/media/missing", authed(http.HandlerFunc(handleListMissing(app))))
	mux.Handle("DELETE /api/media/missing", authed(http.HandlerFunc(handleCleanupMissing(app))))

	mux.Handle("GET /api/media", authed(http.HandlerFunc(handleListMedia(app))))
	mux.Handle("GET /api/media/enriched", authed(http.HandlerFunc(handleListEnrichedMedia(app))))
	mux.Handle("GET /api/media/{id}", authed(http.HandlerFunc(handleGetMedia(app))))
	mux.Handle("GET /api/person/{id}", authed(http.HandlerFunc(handleGetPerson(app))))
	mux.Handle("POST /api/media/{id}/refresh", authed(http.HandlerFunc(handleRefreshMedia(app))))
	mux.Handle("GET /api/search", authed(http.HandlerFunc(handleSearchAll(app))))
	mux.Handle("GET /api/search/quick", authed(http.HandlerFunc(handleSearchQuick(app))))

	mux.Handle("GET /api/genres", authed(http.HandlerFunc(handleListGenres(app))))
	mux.Handle("GET /api/genres/{name}", authed(http.HandlerFunc(handleGetGenre(app))))
	mux.Handle("GET /api/keywords/{name}", authed(http.HandlerFunc(handleGetKeyword(app))))
	mux.Handle("GET /api/collections", authed(http.HandlerFunc(handleListCollections(app))))
	mux.Handle("GET /api/collections/{id}", authed(http.HandlerFunc(handleGetCollection(app))))
	mux.Handle("GET /api/collections/browse", authed(http.HandlerFunc(handleBrowseCollections(app))))

	// Filter typeahead search
	mux.Handle("GET /api/people/search", authed(http.HandlerFunc(handleSearchPeople(app))))
	mux.Handle("POST /api/people/media-ids", authed(http.HandlerFunc(handlePeopleMediaIDs(app))))
	mux.Handle("GET /api/studios/search", authed(http.HandlerFunc(handleSearchStudios(app))))
	mux.Handle("POST /api/studios/media-ids", authed(http.HandlerFunc(handleStudioMediaIDs(app))))

	mux.Handle("GET /api/watchers", authed(http.HandlerFunc(handleWatcherStatus(app))))

	// Recommendations
	mux.Handle("GET /api/recommendations", authed(http.HandlerFunc(handleListTopRecommendations(app))))

	// Activity feed
	mux.Handle("GET /api/activity", authed(http.HandlerFunc(handleActivityFeed(app))))

	// Filesystem browser
	mux.Handle("GET /api/fs/browse", authed(http.HandlerFunc(handleFSBrowse(app))))

	// Transcoding settings
	mux.Handle("GET /api/transcode/status", authed(http.HandlerFunc(handleGetTranscodeStatus(app))))
	mux.Handle("PUT /api/transcode/settings", authed(http.HandlerFunc(handleUpdateTranscodeSettings(app))))
	mux.Handle("DELETE /api/transcode/cache", authed(http.HandlerFunc(handleClearTranscodeCache(app))))

	// Streaming
	mux.Handle("GET /api/stream/{file_id}", authed(http.HandlerFunc(handleDirectStream(app))))
	mux.Handle("GET /api/stream/{file_id}/hls/master.m3u8", authed(http.HandlerFunc(handleHLSMaster(app))))
	mux.Handle("GET /api/stream/{file_id}/hls/index.m3u8", authed(http.HandlerFunc(handleHLSPlaylist(app))))
	mux.Handle("GET /api/stream/{file_id}/hls/{segment}", authed(http.HandlerFunc(handleHLSSegment(app))))

	// Stream info & subtitles
	mux.Handle("GET /api/stream/{file_id}/info", authed(http.HandlerFunc(handleGetStreamInfo(app))))
	mux.Handle("GET /api/stream/{file_id}/transcode-status", authed(http.HandlerFunc(handleTranscodeStatus(app))))
	mux.Handle("GET /api/stream/{file_id}/subtitles", authed(http.HandlerFunc(handleListSubtitles(app))))
	mux.Handle("GET /api/stream/{file_id}/subtitles/{index}", authed(http.HandlerFunc(handleGetSubtitle(app))))

	// Trickplay
	mux.Handle("GET /api/stream/{file_id}/trickplay/index.vtt", authed(http.HandlerFunc(handleTrickplayVTT(app))))
	mux.Handle("GET /api/stream/{file_id}/trickplay/{filename}", authed(http.HandlerFunc(handleTrickplaySprite(app))))

	// Extra thumbnails
	mux.HandleFunc("GET /api/extras/{id}/thumbnail", handleExtraThumbnail(app))

	// Watch progress
	mux.Handle("POST /api/watch/{media_item_id}/progress", authed(http.HandlerFunc(handleWatchProgress(app))))
	mux.Handle("POST /api/watch/progress", authed(http.HandlerFunc(handleWatchProgress(app))))
	mux.Handle("GET /api/watch/continue", authed(http.HandlerFunc(handleContinueWatching(app))))
	mux.Handle("GET /api/watch/recent", authed(http.HandlerFunc(handleRecentlyWatched(app))))
	mux.Handle("GET /api/watch/history", authed(http.HandlerFunc(handleWatchHistory(app))))

	// Favorites
	mux.Handle("POST /api/favorites/toggle", authed(http.HandlerFunc(handleToggleFavorite(app))))
	mux.Handle("GET /api/favorites/check", authed(http.HandlerFunc(handleCheckFavorite(app))))

	// User lists
	mux.Handle("GET /api/lists", authed(http.HandlerFunc(handleListUserLists(app))))
	mux.Handle("POST /api/lists", authed(http.HandlerFunc(handleCreateUserList(app))))
	mux.Handle("GET /api/lists/{id}", authed(http.HandlerFunc(handleGetUserList(app))))
	mux.Handle("PUT /api/lists/{id}", authed(http.HandlerFunc(handleUpdateUserList(app))))
	mux.Handle("DELETE /api/lists/{id}", authed(http.HandlerFunc(handleDeleteUserList(app))))
	mux.Handle("POST /api/lists/{id}/items", authed(http.HandlerFunc(handleAddToList(app))))
	mux.Handle("DELETE /api/lists/{id}/items/{media_id}", authed(http.HandlerFunc(handleRemoveFromList(app))))
	mux.Handle("PUT /api/lists/{id}/reorder", authed(http.HandlerFunc(handleReorderList(app))))

	// Episode watched tracking
	mux.Handle("POST /api/episodes/{episode_id}/watched", authed(http.HandlerFunc(handleMarkEpisodeWatched(app))))
	mux.Handle("DELETE /api/episodes/{episode_id}/watched", authed(http.HandlerFunc(handleUnmarkEpisodeWatched(app))))
	mux.Handle("POST /api/seasons/{season_id}/watched", authed(http.HandlerFunc(handleMarkSeasonWatched(app))))
	mux.Handle("POST /api/media/{id}/watched", authed(http.HandlerFunc(handleMarkShowWatched(app))))
	mux.Handle("POST /api/movies/{id}/watched", authed(http.HandlerFunc(handleMarkMovieWatched(app))))
	mux.Handle("GET /api/media/{id}/watched-episodes", authed(http.HandlerFunc(handleGetWatchedEpisodes(app))))
	mux.Handle("GET /api/media/{id}/up-next", authed(http.HandlerFunc(handleGetUpNext(app))))
	mux.Handle("GET /api/user/media-state", authed(http.HandlerFunc(handleGetUserMediaState(app))))
	mux.Handle("POST /api/user/state", authed(http.HandlerFunc(handleGetUserState(app))))
	mux.Handle("GET /api/user/settings", authed(http.HandlerFunc(handleGetUserSettings(app))))
	mux.Handle("PUT /api/user/settings", authed(http.HandlerFunc(handleUpdateUserSettings(app))))
	mux.Handle("GET /api/user/playback/{media_id}", authed(http.HandlerFunc(handleGetPlaybackPreference(app))))
	mux.Handle("PUT /api/user/playback/{media_id}", authed(http.HandlerFunc(handleSetPlaybackPreference(app))))
	mux.Handle("GET /api/media/{id}/languages", authed(http.HandlerFunc(handleGetMediaLanguages(app))))

	// Jobs & schedules
	mux.Handle("GET /api/jobs", authed(http.HandlerFunc(handleListJobs(app))))
	mux.Handle("GET /api/jobs/summary", authed(http.HandlerFunc(handleJobSummary(app))))
	mux.Handle("POST /api/jobs/rescue", authed(http.HandlerFunc(handleRescueJobs(app))))
	mux.Handle("POST /api/jobs/{id}/retry", authed(http.HandlerFunc(handleRetryJob(app))))
	mux.Handle("POST /api/jobs/{id}/cancel", authed(http.HandlerFunc(handleCancelJob(app))))
	mux.Handle("DELETE /api/jobs/completed", authed(http.HandlerFunc(handleClearJobs(app))))
	mux.Handle("DELETE /api/jobs", authed(http.HandlerFunc(handleClearAllJobs(app))))
	mux.Handle("GET /api/schedules", authed(http.HandlerFunc(handleListSchedules(app))))

	// Scheduled tasks
	mux.Handle("GET /api/tasks", authed(http.HandlerFunc(handleListTasks(app))))
	mux.Handle("GET /api/tasks/{id}/items", authed(http.HandlerFunc(handleTaskItems(app))))
	mux.Handle("POST /api/tasks/{id}/run", authed(http.HandlerFunc(handleRunTask(app))))
	mux.Handle("POST /api/tasks/{id}/cancel", authed(http.HandlerFunc(handleCancelTask(app))))
	mux.Handle("PUT /api/tasks/{id}", authed(http.HandlerFunc(handleUpdateTask(app))))
}

func registerLogRoutes(mux *http.ServeMux, app *service.App, buf *logbuf.RingBuffer) {
	authed := auth.Middleware(app.SessionLookup())
	mux.Handle("GET /api/logs", authed(http.HandlerFunc(handleGetLogs(buf))))
	mux.Handle("GET /api/logs/stream", authed(http.HandlerFunc(handleLogStream(buf))))
}

func registerHumaRoutes(api huma.API, app *service.App) {
	// OpenAPI spec is auto-generated from these operation registrations.
	// The actual request handling is done by the existing handlers above
	// via the stdlib mux. These Huma registrations provide the schema.

	// --- System ---
	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/api/health",
		Summary:     "Health check",
		Description: "Returns server and database connection status.",
		Tags:        []string{"System"},
	}, func(ctx context.Context, input *struct{}) (*HealthOutput, error) {
		return nil, nil // handled by stdlib handler
	})

	// --- Authentication ---
	huma.Register(api, huma.Operation{
		OperationID: "register",
		Method:      http.MethodPost,
		Path:        "/api/auth/register",
		Summary:     "Register a new user",
		Description: "Create a new user account. The first user automatically becomes admin.",
		Tags:        []string{"Authentication"},
	}, func(ctx context.Context, input *RegisterInput) (*AuthTokenOutput, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/auth/login",
		Summary:     "Login",
		Description: "Authenticate with username and password. Returns a session token.",
		Tags:        []string{"Authentication"},
	}, func(ctx context.Context, input *LoginInput) (*AuthTokenOutput, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-libraries",
		Method:      http.MethodGet,
		Path:        "/api/libraries",
		Summary:     "List all libraries",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-library",
		Method:      http.MethodPost,
		Path:        "/api/libraries",
		Summary:     "Create a library",
		Description: "Add a new media library with one or more filesystem paths.",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *CreateLibraryInput) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-library",
		Method:      http.MethodGet,
		Path:        "/api/libraries/{id}",
		Summary:     "Get library details",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *LibraryIDParam) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "scan-library",
		Method:      http.MethodPost,
		Path:        "/api/libraries/{id}/scan",
		Summary:     "Scan a library",
		Description: "Walk filesystem paths, discover media files, and match to metadata providers. Use ?async=true to enqueue as background job.",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		LibraryIDParam
		AsyncParam
	}) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-library-files",
		Method:      http.MethodGet,
		Path:        "/api/libraries/{id}/files",
		Summary:     "List files in a library",
		Description: "Returns paginated list of discovered media files with parse results and status.",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		LibraryIDParam
		PaginationParams
	}) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "library-file-stats",
		Method:      http.MethodGet,
		Path:        "/api/libraries/{id}/files/stats",
		Summary:     "File status statistics",
		Description: "Returns count of files grouped by status (matched, unmatched, pending, error).",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *LibraryIDParam) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-unmatched",
		Method:      http.MethodGet,
		Path:        "/api/libraries/{id}/unmatched",
		Summary:     "List unmatched files with candidates",
		Description: "Returns files that could not be auto-matched, along with their match candidates for manual resolution.",
		Tags:        []string{"Libraries"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *LibraryIDParam) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "resolve-match",
		Method:      http.MethodPost,
		Path:        "/api/library-files/{id}/resolve",
		Summary:     "Resolve a file match",
		Description: "Accept a match candidate for an unmatched file, creating the media item and linking it.",
		Tags:        []string{"Matching"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		FileIDParam
		ResolveMatchInput
	}) (*struct{ Body any }, error) {
		return nil, nil
	})

	// --- Media ---
	huma.Register(api, huma.Operation{
		OperationID: "list-media",
		Method:      http.MethodGet,
		Path:        "/api/media",
		Summary:     "List media items",
		Description: "Returns paginated list of matched media items filtered by type.",
		Tags:        []string{"Media"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		MediaTypeFilter
		PaginationParams
	}) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-media",
		Method:      http.MethodGet,
		Path:        "/api/media/{id}",
		Summary:     "Get media item details",
		Description: "Returns full media item with type-specific metadata (movie, TV, music, or book details).",
		Tags:        []string{"Media"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *MediaIDParam) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "search-media",
		Method:      http.MethodGet,
		Path:        "/api/search",
		Summary:     "Search media items",
		Description: "Full-text search across all media items using PostgreSQL tsvector.",
		Tags:        []string{"Media"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *SearchQueryParam) (*struct{ Body []any }, error) {
		return nil, nil
	})

	// --- System ---
	huma.Register(api, huma.Operation{
		OperationID: "watcher-status",
		Method:      http.MethodGet,
		Path:        "/api/watchers",
		Summary:     "Filesystem watcher status",
		Description: "Returns list of active filesystem watchers monitoring library paths.",
		Tags:        []string{"System"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct{ Body any }, error) {
		return nil, nil
	})

	// --- Future API namespaces ---
	// registerJellyfinRoutes(api, app)  // /jellyfin/... — Jellyfin-compatible API
	// registerSubsonicRoutes(api, app)  // /rest/... — Subsonic API for music clients
	// registerAudiomuseRoutes(api, app) // /api/audiomuse/... — AI music analysis
}
