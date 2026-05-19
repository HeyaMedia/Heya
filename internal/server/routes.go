package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/kura/internal/auth"
	"github.com/karbowiak/kura/internal/service"
)

func registerRoutes(mux *http.ServeMux, app *service.App) {
	mux.HandleFunc("GET /api/health", healthHandler(app.DB))

	mux.HandleFunc("POST /api/auth/register", handleRegister(app))
	mux.HandleFunc("POST /api/auth/login", handleLogin(app))
	mux.HandleFunc("POST /api/auth/logout", handleLogout(app))

	authed := auth.Middleware(app.Queries())

	mux.Handle("GET /api/auth/me", authed(http.HandlerFunc(handleMe(app))))

	mux.Handle("POST /api/libraries", authed(http.HandlerFunc(handleCreateLibrary(app))))
	mux.Handle("GET /api/libraries", authed(http.HandlerFunc(handleListLibraries(app))))
	mux.Handle("GET /api/libraries/{id}", authed(http.HandlerFunc(handleGetLibrary(app))))
	mux.Handle("PUT /api/libraries/{id}", authed(http.HandlerFunc(handleUpdateLibrary(app))))
	mux.Handle("DELETE /api/libraries/{id}", authed(http.HandlerFunc(handleDeleteLibrary(app))))

	mux.Handle("POST /api/libraries/{id}/scan", authed(http.HandlerFunc(handleScanLibrary(app))))
	mux.Handle("GET /api/libraries/{id}/files", authed(http.HandlerFunc(handleListLibraryFiles(app))))
	mux.Handle("GET /api/libraries/{id}/files/stats", authed(http.HandlerFunc(handleLibraryFileStats(app))))
	mux.Handle("GET /api/libraries/{id}/unmatched", authed(http.HandlerFunc(handleListUnmatched(app))))

	mux.Handle("POST /api/library-files/{id}/resolve", authed(http.HandlerFunc(handleResolveMatch(app))))

	mux.Handle("GET /api/media", authed(http.HandlerFunc(handleListMedia(app))))
	mux.Handle("GET /api/media/{id}", authed(http.HandlerFunc(handleGetMedia(app))))
	mux.Handle("GET /api/search", authed(http.HandlerFunc(handleSearchMedia(app))))

	mux.Handle("GET /api/watchers", authed(http.HandlerFunc(handleWatcherStatus(app))))
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
		OperationID:   "list-libraries",
		Method:        http.MethodGet,
		Path:          "/api/libraries",
		Summary:       "List all libraries",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-library",
		Method:        http.MethodPost,
		Path:          "/api/libraries",
		Summary:       "Create a library",
		Description:   "Add a new media library with one or more filesystem paths.",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *CreateLibraryInput) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "get-library",
		Method:        http.MethodGet,
		Path:          "/api/libraries/{id}",
		Summary:       "Get library details",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *LibraryIDParam) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "scan-library",
		Method:        http.MethodPost,
		Path:          "/api/libraries/{id}/scan",
		Summary:       "Scan a library",
		Description:   "Walk filesystem paths, discover media files, and match to metadata providers. Use ?async=true to enqueue as background job.",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		LibraryIDParam
		AsyncParam
	}) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "list-library-files",
		Method:        http.MethodGet,
		Path:          "/api/libraries/{id}/files",
		Summary:       "List files in a library",
		Description:   "Returns paginated list of discovered media files with parse results and status.",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		LibraryIDParam
		PaginationParams
	}) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "library-file-stats",
		Method:        http.MethodGet,
		Path:          "/api/libraries/{id}/files/stats",
		Summary:       "File status statistics",
		Description:   "Returns count of files grouped by status (matched, unmatched, pending, error).",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *LibraryIDParam) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "list-unmatched",
		Method:        http.MethodGet,
		Path:          "/api/libraries/{id}/unmatched",
		Summary:       "List unmatched files with candidates",
		Description:   "Returns files that could not be auto-matched, along with their match candidates for manual resolution.",
		Tags:          []string{"Libraries"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *LibraryIDParam) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "resolve-match",
		Method:        http.MethodPost,
		Path:          "/api/library-files/{id}/resolve",
		Summary:       "Resolve a file match",
		Description:   "Accept a match candidate for an unmatched file, creating the media item and linking it.",
		Tags:          []string{"Matching"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		FileIDParam
		ResolveMatchInput
	}) (*struct{ Body any }, error) {
		return nil, nil
	})

	// --- Media ---
	huma.Register(api, huma.Operation{
		OperationID:   "list-media",
		Method:        http.MethodGet,
		Path:          "/api/media",
		Summary:       "List media items",
		Description:   "Returns paginated list of matched media items filtered by type.",
		Tags:          []string{"Media"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct {
		MediaTypeFilter
		PaginationParams
	}) (*struct{ Body []any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "get-media",
		Method:        http.MethodGet,
		Path:          "/api/media/{id}",
		Summary:       "Get media item details",
		Description:   "Returns full media item with type-specific metadata (movie, TV, music, or book details).",
		Tags:          []string{"Media"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *MediaIDParam) (*struct{ Body any }, error) {
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "search-media",
		Method:        http.MethodGet,
		Path:          "/api/search",
		Summary:       "Search media items",
		Description:   "Full-text search across all media items using PostgreSQL tsvector.",
		Tags:          []string{"Media"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *SearchQueryParam) (*struct{ Body []any }, error) {
		return nil, nil
	})

	// --- System ---
	huma.Register(api, huma.Operation{
		OperationID:   "watcher-status",
		Method:        http.MethodGet,
		Path:          "/api/watchers",
		Summary:       "Filesystem watcher status",
		Description:   "Returns list of active filesystem watchers monitoring library paths.",
		Tags:          []string{"System"},
		Security:      []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *struct{}) (*struct{ Body any }, error) {
		return nil, nil
	})

	// --- Future API namespaces ---
	// registerJellyfinRoutes(api, app)  // /jellyfin/... — Jellyfin-compatible API
	// registerSubsonicRoutes(api, app)  // /rest/... — Subsonic API for music clients
	// registerAudiomuseRoutes(api, app) // /api/audiomuse/... — AI music analysis
}
