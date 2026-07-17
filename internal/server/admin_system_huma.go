package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// registerAdminSystemRoutes mounts the admin-only system-surface endpoints
// the PR 4 settings pages consume: runtime/system metadata, storage usage,
// database stats, listener inventory, runtime log-level control, and the
// admin-wide session roster.
//
// Each handler keeps its work cheap — no shell-outs to `du`, no full library
// walks — because the dashboard polls these. Storage-walking is bounded to a
// single Statfs per path; database stats are a single pg_database_size + the
// pool's in-memory stat struct.
func registerAdminSystemRoutes(api huma.API, app *service.App, hub *eventhub.Hub) {
	// --- System: runtime + build info ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/system", "admin-system", "Process + runtime metadata", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminSystemBody], error) {
			return noStoreJSON(collectAdminSystem(app, hub)), nil
		})

	// --- Storage: per-library + data dir + transcode cache ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/storage", "admin-storage", "Storage usage for the data dir and every library path", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminStorageBody], error) {
			return noStoreJSON(collectAdminStorage(ctx, app)), nil
		})

	// Trigger a background walk of every library path (or just one when
	// library_id is provided) to populate library_disk_usage. UniqueByArgs
	// on the worker means duplicate clicks are no-ops.
	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/storage/scan", "admin-storage-scan", "Kick off a disk-usage walk of library paths", "Admin")),
		func(ctx context.Context, in *struct {
			Body struct {
				LibraryID int64 `json:"library_id,omitempty" doc:"Scan a single library; omit to scan all" minimum:"0"`
			}
		}) (*StatusOutput, error) {
			if err := app.EnqueueScanLibraryDisk(ctx, in.Body.LibraryID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("queued"), nil
		})

	// --- Database: pg version, size, pool stats ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/db", "admin-db", "Database size, pool stats, version", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminDBBody], error) {
			return noStoreJSON(collectAdminDB(ctx, app)), nil
		})

	// --- Listeners: LAN + tailscale exposure ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/listeners", "admin-listeners", "HTTP / WS listener inventory", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminListenersBody], error) {
			return noStoreJSON(collectAdminListeners(app, hub)), nil
		})

	// --- Log level: global zerolog level (read + write) ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/log-level", "admin-get-log-level", "Current global log level", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[adminLogLevelBody], error) {
			return noStoreJSON(adminLogLevelBody{
				Level:     zerolog.GlobalLevel().String(),
				Available: logLevels,
				BootLevel: app.ConfigSnapshot().LogLevel.Value,
			}), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/admin/log-level", "admin-set-log-level", "Set the global log level at runtime", "Admin")),
		func(ctx context.Context, in *struct {
			Body struct {
				Level string `json:"level" enum:"trace,debug,info,warn,error,fatal,panic,disabled" doc:"New zerolog level"`
			}
		}) (*JSONOutput[adminLogLevelBody], error) {
			lvl, err := zerolog.ParseLevel(in.Body.Level)
			if err != nil {
				return nil, huma.Error400BadRequest("invalid level: " + in.Body.Level)
			}
			zerolog.SetGlobalLevel(lvl)
			log.Info().Str("level", lvl.String()).Msg("log level changed at runtime")
			return noStoreJSON(adminLogLevelBody{
				Level:     lvl.String(),
				Available: logLevels,
				BootLevel: app.ConfigSnapshot().LogLevel.Value,
			}), nil
		})

	// --- Users: admin-only roster + CRUD ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/users", "admin-list-users", "Every user account", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]adminUserView], error) {
			users, err := app.ListUsers(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			out := make([]adminUserView, 0, len(users))
			for _, u := range users {
				out = append(out, toAdminUserView(u))
			}
			return noStoreJSON(out), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/users", "admin-create-user", "Create a new user", "Admin")),
		func(ctx context.Context, in *struct {
			Body struct {
				Username string `json:"username" minLength:"1" maxLength:"64" example:"alice"`
				Email    string `json:"email" minLength:"1" maxLength:"254" format:"email" example:"alice@example.com"`
				Password string `json:"password" minLength:"8" maxLength:"256" example:"hunter2hunter2"`
				IsAdmin  bool   `json:"is_admin"`
			}
		}) (*JSONOutput[adminUserView], error) {
			user, err := app.CreateUser(ctx, in.Body.Username, in.Body.Email, in.Body.Password, in.Body.IsAdmin)
			if err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}
			return noStoreJSON(toAdminUserView(user)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/admin/users/{id}", "admin-delete-user", "Delete a user (and cascade their sessions)", "Admin")),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id" minimum:"1"`
		}) (*StatusOutput, error) {
			if err := app.DeleteUserByID(ctx, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("deleted"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPatch, "/api/admin/users/{id}/role", "admin-set-user-role", "Toggle a user's admin flag", "Admin")),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id" minimum:"1"`
			Body struct {
				IsAdmin bool `json:"is_admin"`
			}
		}) (*JSONOutput[adminUserView], error) {
			user, err := app.SetUserAdmin(ctx, in.ID, in.Body.IsAdmin)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(toAdminUserView(user)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/users/{id}/password", "admin-reset-user-password", "Reset a user's password (admin override)", "Admin")),
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id" minimum:"1"`
			Body struct {
				NewPassword string `json:"new_password" minLength:"8" maxLength:"256"`
			}
		}) (*StatusOutput, error) {
			if err := app.ResetPasswordByID(ctx, in.ID, in.Body.NewPassword); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("password reset"), nil
		})

	// --- Sessions: list + revoke any (admin-wide) ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/sessions", "admin-list-sessions", "All sessions across every user", "Admin")),
		simpleGet(app.ListAllSessionsForAdmin, 0))

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/admin/sessions/{id}", "admin-revoke-session", "Revoke any session by id", "Admin")),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id" minimum:"1" doc:"Session id"`
		}) (*StatusOutput, error) {
			if err := app.RevokeAnySession(ctx, in.ID); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("revoked"), nil
		})
}

// --- /api/admin/users ---

type adminUserView struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt string `json:"created_at"`
}

func toAdminUserView(u sqlc.User) adminUserView {
	return adminUserView{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		IsAdmin:   u.IsAdmin,
		CreatedAt: u.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// --- /api/admin/system ---

type adminSystemBody struct {
	Hostname       string         `json:"hostname"`
	PID            int            `json:"pid"`
	StartedAt      string         `json:"started_at" example:"2026-05-25T08:14:01Z"`
	UptimeSeconds  int64          `json:"uptime_seconds"`
	GoVersion      string         `json:"go_version"`
	GOOS           string         `json:"goos"`
	GOARCH         string         `json:"goarch"`
	NumCPU         int            `json:"num_cpu"`
	GOMAXPROCS     int            `json:"gomaxprocs"`
	Goroutines     int            `json:"goroutines"`
	HeapInUseBytes uint64         `json:"heap_inuse_bytes"`
	HeapAllocBytes uint64         `json:"heap_alloc_bytes"`
	SysBytes       uint64         `json:"sys_bytes"`
	StackBytes     uint64         `json:"stack_bytes"`
	NumGC          uint32         `json:"num_gc"`
	GCPauseLastNs  uint64         `json:"gc_pause_last_ns"`
	NumCgoCall     int64          `json:"num_cgo_call"`
	WSSubscribers  int            `json:"ws_subscribers"`
	Build          map[string]any `json:"build,omitempty"`
}

func collectAdminSystem(app *service.App, hub *eventhub.Hub) adminSystemBody {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	host, _ := os.Hostname()
	started := app.StartedAt()
	body := adminSystemBody{
		Hostname:       host,
		PID:            os.Getpid(),
		StartedAt:      started.UTC().Format(time.RFC3339),
		UptimeSeconds:  int64(time.Since(started).Seconds()),
		GoVersion:      runtime.Version(),
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		NumCPU:         runtime.NumCPU(),
		GOMAXPROCS:     runtime.GOMAXPROCS(0),
		Goroutines:     runtime.NumGoroutine(),
		HeapInUseBytes: ms.HeapInuse,
		HeapAllocBytes: ms.HeapAlloc,
		SysBytes:       ms.Sys,
		StackBytes:     ms.StackInuse,
		NumGC:          ms.NumGC,
		GCPauseLastNs:  ms.PauseNs[(ms.NumGC+255)%256],
		NumCgoCall:     runtime.NumCgoCall(),
	}
	if hub != nil {
		body.WSSubscribers = hub.SubscriberCount()
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		build := map[string]any{
			"path":    bi.Path,
			"version": bi.Main.Version,
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.modified", "vcs.time", "CGO_ENABLED":
				build[s.Key] = s.Value
			}
		}
		body.Build = build
	}
	return body
}

// --- /api/admin/storage ---

type adminStoragePath struct {
	Label      string `json:"label"`
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	IsDir      bool   `json:"is_dir"`
	TotalBytes uint64 `json:"total_bytes,omitempty"`
	FreeBytes  uint64 `json:"free_bytes,omitempty"`
	UsedBytes  uint64 `json:"used_bytes,omitempty"`
	UsedPct    int    `json:"used_pct,omitempty"`
	Error      string `json:"error,omitempty"`
}

type adminStorageBody struct {
	DataDir          string                     `json:"data_dir"`
	TranscodeDir     string                     `json:"transcode_dir"`
	TranscodeUsedMB  int64                      `json:"transcode_used_mb"`
	TranscodeMaxGB   int                        `json:"transcode_max_gb"`
	TranscodeItems   int64                      `json:"transcode_items"`
	LibraryPaths     []adminStoragePath         `json:"library_paths"`
	DataDirVolume    adminStoragePath           `json:"data_dir_volume"`
	TranscodeVolume  adminStoragePath           `json:"transcode_volume"`
	LibraryDiskUsage []service.LibraryDiskUsage `json:"library_disk_usage" doc:"Cached results from the last scan_library_disk run; empty until a scan completes"`
}

func collectAdminStorage(ctx context.Context, app *service.App) adminStorageBody {
	cfg := app.ConfigSnapshot()
	body := adminStorageBody{
		DataDir:         cfg.DataDir.Value,
		TranscodeDir:    cfg.TranscodeCacheDir.Value,
		TranscodeMaxGB:  cfg.TranscodeCacheMaxGB.Value,
		DataDirVolume:   pathStorage("Data dir", cfg.DataDir.Value),
		TranscodeVolume: pathStorage("Transcode cache", cfg.TranscodeCacheDir.Value),
	}

	if tc := app.TranscoderCache(); tc != nil {
		st := tc.Stats()
		body.TranscodeUsedMB = st.TotalSize / (1024 * 1024)
		body.TranscodeItems = int64(st.ItemCount)
	}

	libs, err := app.ListLibraries(ctx)
	if err == nil {
		body.LibraryPaths = make([]adminStoragePath, 0, len(libs))
		for _, lib := range libs {
			for _, p := range lib.Paths {
				body.LibraryPaths = append(body.LibraryPaths, pathStorage(lib.Name, p))
			}
		}
	}

	if usage, err := app.ListLibraryDiskUsage(ctx); err == nil {
		body.LibraryDiskUsage = usage
	}
	return body
}

// pathStorage gathers presence + filesystem-level usage stats for a path.
// Doesn't walk the directory — that would block on multi-TB libraries.
// Filesystem totals come from statfs, which is the same data `df` reports.
func pathStorage(label, p string) adminStoragePath {
	out := adminStoragePath{Label: label, Path: p}
	info, err := os.Stat(p)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Exists = true
	out.IsDir = info.IsDir()

	var stat syscall.Statfs_t
	if err := syscall.Statfs(p, &stat); err != nil {
		out.Error = err.Error()
		return out
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	out.TotalBytes = total
	out.FreeBytes = free
	out.UsedBytes = used
	if total > 0 {
		out.UsedPct = int(used * 100 / total)
	}
	return out
}

// --- /api/admin/db ---

type adminDBTable struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

type adminDBBody struct {
	Version              string         `json:"version"`
	DatabaseName         string         `json:"database_name"`
	SizeBytes            int64          `json:"size_bytes"`
	TotalConnections     int32          `json:"total_connections"`
	AcquiredConnections  int32          `json:"acquired_connections"`
	IdleConnections      int32          `json:"idle_connections"`
	MaxConnections       int32          `json:"max_connections"`
	AcquireCount         int64          `json:"acquire_count"`
	AcquireDurationMs    int64          `json:"acquire_duration_ms"`
	CanceledAcquireCount int64          `json:"canceled_acquire_count"`
	EmptyAcquireCount    int64          `json:"empty_acquire_count"`
	TopTables            []adminDBTable `json:"top_tables"`
	Error                string         `json:"error,omitempty"`
}

func collectAdminDB(ctx context.Context, app *service.App) adminDBBody {
	body := adminDBBody{}
	pool := app.DBPool()
	if pool == nil {
		body.Error = "no database pool"
		return body
	}

	st := pool.Stat()
	body.TotalConnections = st.TotalConns()
	body.AcquiredConnections = st.AcquiredConns()
	body.IdleConnections = st.IdleConns()
	body.MaxConnections = st.MaxConns()
	body.AcquireCount = st.AcquireCount()
	body.AcquireDurationMs = st.AcquireDuration().Milliseconds()
	body.CanceledAcquireCount = st.CanceledAcquireCount()
	body.EmptyAcquireCount = st.EmptyAcquireCount()

	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&body.DatabaseName); err != nil {
		body.Error = err.Error()
		return body
	}

	// Postgres version() returns a verbose string; trim the leading "PostgreSQL"
	// to keep the dashboard readable.
	var rawVersion string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&rawVersion); err == nil {
		body.Version = strings.TrimPrefix(rawVersion, "PostgreSQL ")
	}

	if err := pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&body.SizeBytes); err != nil && body.Error == "" {
		body.Error = err.Error()
	}

	// Top 10 user tables by total size (table + indexes + toast). Skips the
	// pg_catalog and information_schema so the dashboard reflects Heya data,
	// not pg bookkeeping.
	const topTablesSQL = `
		SELECT schemaname || '.' || relname AS name,
		       pg_total_relation_size(relid) AS size
		FROM pg_catalog.pg_statio_user_tables
		ORDER BY size DESC
		LIMIT 10
	`
	rows, err := pool.Query(ctx, topTablesSQL)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t adminDBTable
			if err := rows.Scan(&t.Name, &t.SizeBytes); err != nil {
				break
			}
			body.TopTables = append(body.TopTables, t)
		}
	}
	return body
}

// --- /api/admin/listeners ---

type adminListener struct {
	Name        string   `json:"name,omitempty"`
	Kind        string   `json:"kind"` // "lan" | "remote" | "tailscale" | "funnel"
	Network     string   `json:"network,omitempty"`
	Address     string   `json:"address"`
	Protocols   []string `json:"protocols,omitempty"`
	TLS         bool     `json:"tls"`
	Public      bool     `json:"public"`
	Active      bool     `json:"active"`
	Description string   `json:"description,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type adminListenersBody struct {
	WSSubscribers int             `json:"ws_subscribers"`
	Listeners     []adminListener `json:"listeners"`
}

func collectAdminListeners(app *service.App, hub *eventhub.Hub) adminListenersBody {
	cfg := app.ConfigSnapshot()
	body := adminListenersBody{}
	if hub != nil {
		body.WSSubscribers = hub.SubscriberCount()
	}
	if manager := app.Ingress(); manager != nil {
		for _, listener := range manager.Status().Listeners {
			kind := listener.Kind
			if kind == "host" {
				kind = "lan"
			}
			body.Listeners = append(body.Listeners, adminListener{
				Name: listener.Name, Kind: kind, Network: listener.Network,
				Address: listener.Address, Protocols: listener.Protocols,
				TLS: listener.TLS, Public: listener.Public, Active: listener.Active,
				Description: listener.Description, Error: listener.Error,
			})
		}
		return body
	}

	body.Listeners = append(body.Listeners, adminListener{
		Kind:        "lan",
		Address:     cfg.Addr(),
		TLS:         false,
		Public:      false,
		Active:      true,
		Description: fmt.Sprintf("LAN listener bound on %s", cfg.Addr()),
	})

	if cfg.Tailscale.Enabled.Value && app.Tailscale() != nil {
		st := app.Tailscale().Status()
		host := st.Hostname
		if host == "" {
			host = cfg.Tailscale.Hostname.Value
		}
		body.Listeners = append(body.Listeners, adminListener{
			Kind:        "tailscale",
			Address:     host,
			TLS:         st.HTTPSActive,
			Public:      st.FunnelActive,
			Description: fmt.Sprintf("Tailnet listener · MagicDNS %s", st.MagicDNS),
		})
	}
	return body
}

// --- /api/admin/log-level ---

var logLevels = []string{"trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled"}

type adminLogLevelBody struct {
	Level     string   `json:"level"`
	BootLevel string   `json:"boot_level" doc:"Level loaded from HEYA_LOG_LEVEL at boot"`
	Available []string `json:"available"`
}
