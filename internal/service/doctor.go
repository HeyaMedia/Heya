package service

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// DoctorReport is the full `heya doctor` support bundle — everything a
// maintainer needs to diagnose a remote install in one paste. Built by
// BuildDoctorReport, exposed via `heya doctor [--json]` and
// GET /api/admin/doctor (see internal/server/admin_doctor_huma.go).
//
// Read-only, always: no section here may write to the DB, touch a file, or
// kick off a scan/walk. It must also be safe to paste publicly — every
// value that could carry a credential is redacted before it lands in the
// struct. See redactConfigValue and scrubCredentials.
type DoctorReport struct {
	GeneratedAt time.Time              `json:"generated_at"`
	App         DoctorAppSection       `json:"app"`
	Config      DoctorConfigSection    `json:"config"`
	Database    DoctorDatabaseSection  `json:"database"`
	Libraries   DoctorLibrariesSection `json:"libraries"`
	Tools       DoctorToolsSection     `json:"tools"`
	Queue       DoctorQueueSection     `json:"queue"`
	Logs        DoctorLogsSection      `json:"logs"`
	Storage     DoctorStorageSection   `json:"storage"`
}

// BuildDoctorReport assembles the full support bundle. buf is the server's
// in-process log ring buffer — pass nil from the CLI (no long-lived process
// to hold one), and the Logs section explains where to get logs instead.
//
// Every section is wrapped in doctorSafe: an unexpected panic in one
// section (nil sub-manager, driver quirk, whatever) is captured into that
// section's Error field instead of aborting the whole report. Handled
// errors (DB unreachable, ffmpeg missing, a library path gone) are recorded
// the same way from inside each section — that's the normal, expected path,
// not the panic backstop.
func (a *App) BuildDoctorReport(ctx context.Context, buf *logbuf.RingBuffer) DoctorReport {
	report := DoctorReport{GeneratedAt: time.Now().UTC()}

	doctorSafe(&report.App.Error, func() { report.App = a.doctorAppSection() })
	doctorSafe(&report.Config.Error, func() { report.Config = a.doctorConfigSection() })
	doctorSafe(&report.Database.Error, func() { report.Database = a.doctorDatabaseSection(ctx) })
	doctorSafe(&report.Libraries.Error, func() { report.Libraries = a.doctorLibrariesSection(ctx) })
	doctorSafe(&report.Tools.Error, func() { report.Tools = doctorToolsSection(ctx) })
	doctorSafe(&report.Queue.Error, func() { report.Queue = a.doctorQueueSection(ctx) })
	doctorSafe(&report.Logs.Error, func() { report.Logs = doctorLogsSection(buf) })
	doctorSafe(&report.Storage.Error, func() { report.Storage = a.doctorStorageSection(ctx) })

	return report
}

// doctorSafe runs fn and turns a panic into *errOut instead of propagating
// it — one section falling over (e.g. a nil pointer somewhere deep) must
// not take down the rest of the bundle.
func doctorSafe(errOut *string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			*errOut = fmt.Sprintf("panic: %v", r)
			log.Error().Interface("panic", r).Msg("doctor: section panicked")
		}
	}()
	fn()
}

// --- redaction helpers ---
//
// The bundle is designed to be pasted into a public bug report, so secrets
// get scrubbed on the way in, not left to the reader's discretion.

// secretConfigKeyPattern flags config keys whose value must never be shown
// verbatim, per the dotted key name (e.g. "tailscale.authkey" would match
// on "key"). It redacts remote.dns_token today and remains a backstop for
// future fields; credentials such as PodcastIndexSecret and Tailscale.AuthKey
// are excluded from Sources() entirely.
var secretConfigKeyPattern = regexp.MustCompile(`(?i)token|key|secret|password|dsn`)

func scrubCredentials(s string) string {
	return secrettext.Redact(s)
}

func redactConfigValue(key, value string) string {
	if secretConfigKeyPattern.MatchString(key) {
		if value == "" {
			return "(empty)"
		}
		return "redacted (set)"
	}
	return scrubCredentials(value)
}

// --- app ---

type DoctorAppSection struct {
	Version       string `json:"version,omitempty"`
	Revision      string `json:"revision,omitempty"`
	RevisionTime  string `json:"revision_time,omitempty"`
	Modified      bool   `json:"modified,omitempty" doc:"Built from a working tree with uncommitted changes"`
	GoVersion     string `json:"go_version"`
	GOOS          string `json:"goos"`
	GOARCH        string `json:"goarch"`
	NumCPU        int    `json:"num_cpu"`
	Hostname      string `json:"hostname"`
	PID           int    `json:"pid"`
	StartedAt     string `json:"started_at,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Error         string `json:"error,omitempty"`
}

func (a *App) doctorAppSection() DoctorAppSection {
	host, _ := os.Hostname()
	started := a.StartedAt()
	out := DoctorAppSection{
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		NumCPU:        runtime.NumCPU(),
		Hostname:      host,
		PID:           os.Getpid(),
		StartedAt:     started.UTC().Format(time.RFC3339),
		UptimeSeconds: int64(time.Since(started).Seconds()),
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		out.Version = bi.Main.Version
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				out.Revision = s.Value
			case "vcs.time":
				out.RevisionTime = s.Value
			case "vcs.modified":
				out.Modified = s.Value == "true"
			}
		}
	}
	return out
}

// --- config ---

type DoctorConfigField struct {
	Value  string        `json:"value"`
	Source config.Source `json:"source"`
	EnvVar string        `json:"env_var,omitempty"`
}

type DoctorConfigSection struct {
	Fields map[string]DoctorConfigField `json:"fields"`
	Error  string                       `json:"error,omitempty"`
}

// doctorConfigSection walks the same provenance map `heya config show` and
// /api/config/sources use (config.Sources()), paired with each field's
// current value — then redacts. See redactConfigValue for what gets hidden.
func (a *App) doctorConfigSection() DoctorConfigSection {
	cfg := a.ConfigSnapshot()
	if cfg == nil {
		return DoctorConfigSection{Error: "configuration unavailable"}
	}
	values := map[string]string{
		"infra.database_url":           cfg.DatabaseURL.Value,
		"infra.database_max_conns":     strconv.Itoa(cfg.DatabaseMaxConns.Value),
		"infra.database_min_conns":     strconv.Itoa(cfg.DatabaseMinConns.Value),
		"infra.passive_mode":           strconv.FormatBool(cfg.PassiveMode.Value),
		"infra.allow_remote_active":    strconv.FormatBool(cfg.AllowRemoteActive.Value),
		"infra.image_proxy_url":        cfg.ImageProxyURL.Value,
		"infra.host":                   cfg.Host.Value,
		"infra.port":                   cfg.Port.Value,
		"infra.log_level":              cfg.LogLevel.Value,
		"security.enable_registration": strconv.FormatBool(cfg.EnableRegistration.Value),
		"security.waf_mode":            cfg.WAFMode.Value,
		"security.trusted_networks":    cfg.TrustedNetworks.Value,
		"infra.log_format":             cfg.LogFormat.Value,
		"infra.data_dir":               cfg.DataDir.Value,
		"infra.heya_metadata_url":      cfg.HeyaMetadataURL.Value,
		"infra.acoustid_base_url":      cfg.AcoustIDBaseURL.Value,
		"infra.acoustid_requests_per_second": strconv.Itoa(
			cfg.AcoustIDRequestsPerSecond.Value,
		),
		"transcoder.hwaccel":      cfg.HWAccel.Value,
		"transcoder.cache_dir":    cfg.TranscodeCacheDir.Value,
		"transcoder.cache_max_gb": strconv.Itoa(cfg.TranscodeCacheMaxGB.Value),
		"jellyfin.enabled":        strconv.FormatBool(cfg.Jellyfin.Enabled.Value),
		"subsonic.enabled":        strconv.FormatBool(cfg.Subsonic.Enabled.Value),
		"cast.enabled":            strconv.FormatBool(cfg.Cast.Enabled.Value),
		"cast.base_url":           cfg.Cast.BaseURL.Value,
		"cast.devices":            cfg.Cast.Devices.Value,
		"tailscale.enabled":       strconv.FormatBool(cfg.Tailscale.Enabled.Value),
		"tailscale.hostname":      cfg.Tailscale.Hostname.Value,
		"tailscale.state_dir":     cfg.Tailscale.StateDir.Value,
		"tailscale.https":         strconv.FormatBool(cfg.Tailscale.HTTPS.Value),
		"tailscale.funnel":        strconv.FormatBool(cfg.Tailscale.Funnel.Value),
		"remote.enabled":          strconv.FormatBool(cfg.Remote.Enabled.Value),
		"remote.port":             strconv.Itoa(cfg.Remote.Port.Value),
		"remote.check_url":        cfg.Remote.CheckURL.Value,
		"remote.cert_dir":         cfg.Remote.CertDir.Value,
		"remote.acme_ca":          cfg.Remote.ACMECA.Value,
		"remote.acme_email":       cfg.Remote.ACMEEmail.Value,
		"remote.dns_provider":     cfg.Remote.DNSProvider.Value,
		"remote.dns_token":        cfg.Remote.DNSToken.Value,
		"remote.domain":           cfg.Remote.Domain.Value,
		"remote.subdomain":        cfg.Remote.Subdomain.Value,
	}
	for kind, field := range cfg.Jobs.Workers {
		values["jobs.workers."+kind] = strconv.Itoa(field.Value)
	}

	sources := cfg.Sources()
	fields := make(map[string]DoctorConfigField, len(sources))
	for k, src := range sources {
		fields[k] = DoctorConfigField{
			Value:  redactConfigValue(k, values[k]),
			Source: src.Source,
			EnvVar: src.EnvVar,
		}
	}
	return DoctorConfigSection{Fields: fields}
}

// --- database ---

type DoctorPoolStats struct {
	TotalConnections    int32 `json:"total_connections"`
	AcquiredConnections int32 `json:"acquired_connections"`
	IdleConnections     int32 `json:"idle_connections"`
	MaxConnections      int32 `json:"max_connections"`
}

type DoctorDatabaseSection struct {
	Reachable        bool             `json:"reachable"`
	Version          string           `json:"version,omitempty"`
	DatabaseName     string           `json:"database_name,omitempty"`
	MigrationVersion int64            `json:"migration_version,omitempty" doc:"Latest applied goose migration id"`
	Pool             DoctorPoolStats  `json:"pool"`
	RowCounts        map[string]int64 `json:"row_counts,omitempty" doc:"Exact for libraries; estimated from pg_class.reltuples for the rest"`
	Error            string           `json:"error,omitempty"`
}

func (a *App) doctorDatabaseSection(ctx context.Context) DoctorDatabaseSection {
	out := DoctorDatabaseSection{}
	pool := a.db
	if pool == nil {
		out.Error = "no database pool"
		return out
	}

	st := pool.Stat()
	out.Pool = DoctorPoolStats{
		TotalConnections:    st.TotalConns(),
		AcquiredConnections: st.AcquiredConns(),
		IdleConnections:     st.IdleConns(),
		MaxConnections:      st.MaxConns(),
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		out.Error = "ping failed: " + err.Error()
		return out
	}
	out.Reachable = true

	_ = pool.QueryRow(ctx, "SELECT current_database()").Scan(&out.DatabaseName)

	var rawVersion string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&rawVersion); err == nil {
		out.Version = strings.TrimPrefix(rawVersion, "PostgreSQL ")
	}

	// goose_db_version always exists once AutoMigrate has run once; ignore
	// the error (leaves MigrationVersion at 0) rather than failing the
	// section over a table that a truly fresh/broken DB might not have yet.
	_ = pool.QueryRow(ctx, "SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1").Scan(&out.MigrationVersion)

	out.RowCounts = doctorRowCounts(ctx, pool)
	return out
}

// doctorRowCounts is exact for the small `libraries` table and estimated
// (via pg_class.reltuples, no seq scan) for everything else — a support
// bundle shouldn't cost a COUNT(*) over a multi-million-row tracks table.
func doctorRowCounts(ctx context.Context, pool *pgxpool.Pool) map[string]int64 {
	counts := make(map[string]int64)

	var exact int64
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM libraries").Scan(&exact); err == nil {
		counts["libraries"] = exact
	}

	for _, table := range []string{"media_items", "tv_seasons", "tv_episodes", "artists", "albums", "tracks"} {
		var estimated int64
		err := pool.QueryRow(ctx,
			`SELECT COALESCE((SELECT reltuples::bigint FROM pg_class WHERE oid = to_regclass($1)), 0)`,
			"public."+table,
		).Scan(&estimated)
		if err == nil {
			counts[table] = estimated
		}
	}
	return counts
}

// --- libraries ---

type DoctorLibraryPath struct {
	Path     string `json:"path" doc:"Configured filesystem path; URL credentials are redacted in legacy invalid values"`
	Exists   bool   `json:"exists"`
	Readable bool   `json:"readable"`
	Error    string `json:"error,omitempty"`
}

type DoctorLibrary struct {
	ID               int64               `json:"id"`
	Name             string              `json:"name"`
	MediaType        string              `json:"media_type"`
	Paths            []DoctorLibraryPath `json:"paths"`
	FileCount        int64               `json:"file_count"`
	FileStatusCounts map[string]int64    `json:"file_status_counts,omitempty" doc:"library_files grouped by status: pending/matched/unmatched/ignored/error"`
	Error            string              `json:"error,omitempty"`
}

type DoctorLibrariesSection struct {
	Libraries         []DoctorLibrary `json:"libraries"`
	MissingMediaTotal int             `json:"missing_media_total" doc:"Global count of media with no live file left (dashboard's cached missing_count) — not broken out per library because that anti-join is only cheap in aggregate"`
	Error             string          `json:"error,omitempty"`
}

func (a *App) doctorLibrariesSection(ctx context.Context) DoctorLibrariesSection {
	out := DoctorLibrariesSection{}
	if a.db == nil {
		out.Error = "no database pool"
		return out
	}
	out.MissingMediaTotal = a.missingCountCached(ctx)

	libs, err := a.ListLibraries(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}

	out.Libraries = make([]DoctorLibrary, 0, len(libs))
	// Path checks run concurrently because mounted network filesystems and
	// large directories can still respond slowly. One degraded mount must not
	// delay diagnostics for every other library.
	var wg sync.WaitGroup
	for _, lib := range libs {
		dl := DoctorLibrary{ID: lib.ID, Name: lib.Name, MediaType: string(lib.MediaType)}
		dl.Paths = make([]DoctorLibraryPath, len(lib.Paths))
		for i, p := range lib.Paths {
			wg.Add(1)
			go func(i int, p string) {
				defer wg.Done()
				dl.Paths[i] = doctorCheckLibraryPath(ctx, p)
			}(i, p)
		}

		if stats, err := a.LibraryFileStats(ctx, lib.ID); err != nil {
			dl.Error = err.Error()
		} else {
			dl.FileStatusCounts = make(map[string]int64, len(stats))
			for _, s := range stats {
				dl.FileStatusCounts[string(s.Status)] = s.Count
				dl.FileCount += s.Count
			}
		}
		out.Libraries = append(out.Libraries, dl)
	}
	wg.Wait()
	return out
}

// doctorCheckLibraryPath reuses the scanner's filesystem entry point so
// "readable" has the same meaning as a real scan. It only stats/lists and
// never writes or triggers a scan.
func doctorCheckLibraryPath(ctx context.Context, p string) DoctorLibraryPath {
	out := DoctorLibraryPath{Path: vfs.RedactPath(p)}
	src, err := vfs.OpenContext(ctx, p)
	if err != nil {
		out.Error = scrubCredentials(err.Error())
		return out
	}
	out.Exists = true

	if _, err := fs.ReadDir(src.FS, "."); err != nil {
		out.Error = scrubCredentials(err.Error())
		return out
	}
	out.Readable = true
	return out
}

// --- tools ---

type DoctorTool struct {
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

type DoctorToolsSection struct {
	FFmpeg  DoctorTool `json:"ffmpeg"`
	FFprobe DoctorTool `json:"ffprobe"`
	Error   string     `json:"error,omitempty"`
}

// doctorToolsSection resolves ffmpeg/ffprobe exactly how the transcoder
// does (exec.LookPath against $PATH — see transcoder.IsFFmpegAvailable) and
// captures the first line of `-version` output. A missing binary is a
// finding to report, not an error to fail the section over.
func doctorToolsSection(ctx context.Context) DoctorToolsSection {
	return DoctorToolsSection{
		FFmpeg:  doctorFFmpegTool(ctx),
		FFprobe: doctorFFprobeTool(ctx),
	}
}

func doctorFFmpegTool(ctx context.Context) DoctorTool {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return DoctorTool{Error: "not found in PATH"}
	}
	out := DoctorTool{Found: true, Path: path}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(cctx, "ffmpeg", "-version").Output()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Version = doctorFirstLine(string(output))
	return out
}

func doctorFFprobeTool(ctx context.Context) DoctorTool {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return DoctorTool{Error: "not found in PATH"}
	}
	out := DoctorTool{Found: true, Path: path}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(cctx, "ffprobe", "-version").Output()
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Version = doctorFirstLine(string(output))
	return out
}

func doctorFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// --- queue ---

type DoctorQueueCount struct {
	Kind  string `json:"kind"`
	State string `json:"state"`
	Count int64  `json:"count"`
}

type DoctorQueueSection struct {
	Counts []DoctorQueueCount `json:"counts,omitempty"`
	Error  string             `json:"error,omitempty"`
}

// doctorQueueSection groups river_job by kind × state, excluding the same
// periodic debounce_sweep noise JobSummary/JobKindSummary hide.
func (a *App) doctorQueueSection(ctx context.Context) DoctorQueueSection {
	out := DoctorQueueSection{}
	if a.db == nil {
		out.Error = "no database pool"
		return out
	}
	rows, err := a.db.Query(ctx, "SELECT kind, state, count(*) FROM river_job WHERE kind <> '"+hiddenJobKind+"' GROUP BY kind, state ORDER BY kind, state")
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var c DoctorQueueCount
		if err := rows.Scan(&c.Kind, &c.State, &c.Count); err != nil {
			out.Error = err.Error()
			break
		}
		out.Counts = append(out.Counts, c)
	}
	return out
}

// --- logs ---

type DoctorLogsSection struct {
	Available bool           `json:"available"`
	Entries   []logbuf.Entry `json:"entries,omitempty"`
	Note      string         `json:"note,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// doctorLogsSection only has something to show when it's called from inside
// the running server process (which owns the ring buffer) — the CLI is a
// short-lived process with no log history of its own.
func doctorLogsSection(buf *logbuf.RingBuffer) DoctorLogsSection {
	if buf == nil {
		return DoctorLogsSection{Note: "not available from CLI; use the API endpoint or Settings → Diagnostics"}
	}
	return DoctorLogsSection{Available: true, Entries: buf.Recent(200)}
}

// --- storage ---

type DoctorPathUsage struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	TotalBytes uint64 `json:"total_bytes,omitempty"`
	FreeBytes  uint64 `json:"free_bytes,omitempty"`
	UsedBytes  uint64 `json:"used_bytes,omitempty"`
	UsedPct    int    `json:"used_pct,omitempty"`
	Error      string `json:"error,omitempty"`
}

type DoctorStorageSection struct {
	DataDir          DoctorPathUsage    `json:"data_dir"`
	TranscodeDir     DoctorPathUsage    `json:"transcode_dir"`
	TranscodeUsedMB  int64              `json:"transcode_used_mb,omitempty"`
	TranscodeItems   int64              `json:"transcode_items,omitempty"`
	LibraryDiskUsage []LibraryDiskUsage `json:"library_disk_usage,omitempty" doc:"Cached results from the last scan_library_disk run; empty until a scan completes. Never triggers a fresh walk."`
	Error            string             `json:"error,omitempty"`
}

func (a *App) doctorStorageSection(ctx context.Context) DoctorStorageSection {
	cfg := a.ConfigSnapshot()
	if cfg == nil {
		return DoctorStorageSection{Error: "configuration unavailable"}
	}
	out := DoctorStorageSection{
		DataDir:      doctorPathUsage(cfg.DataDir.Value),
		TranscodeDir: doctorPathUsage(cfg.TranscodeCacheDir.Value),
	}

	if tc := a.TranscoderCache(); tc != nil {
		st := tc.Stats()
		out.TranscodeUsedMB = st.TotalSize / (1024 * 1024)
		out.TranscodeItems = int64(st.ItemCount)
	} else {
		// Command-mode apps intentionally do not construct playback/cache
		// managers. Read the existing tree directly so doctor still reports
		// useful usage without NewCacheManager's forbidden MkdirAll side effect.
		st := transcoder.ReadCacheStats(cfg.TranscodeCacheDir.Value, cfg.TranscodeCacheMaxGB.Value)
		out.TranscodeUsedMB = st.TotalSize / (1024 * 1024)
		out.TranscodeItems = int64(st.ItemCount)
	}

	usage, err := a.ListLibraryDiskUsage(ctx)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	for i := range usage {
		usage[i].Path = vfs.RedactPath(usage[i].Path)
	}
	out.LibraryDiskUsage = usage
	return out
}

// doctorPathUsage is a statfs-only presence + usage check — same technique
// as the admin dashboard's storage endpoint (internal/server/admin_system_huma.go
// pathStorage), reimplemented here rather than imported to avoid a
// server->service->server import cycle. Never walks the directory tree.
func doctorPathUsage(p string) DoctorPathUsage {
	out := DoctorPathUsage{Path: p}
	if p == "" {
		out.Error = "not configured"
		return out
	}
	if _, err := os.Stat(p); err != nil {
		out.Error = err.Error()
		return out
	}
	out.Exists = true

	var stat syscall.Statfs_t
	if err := syscall.Statfs(p, &stat); err != nil {
		out.Error = err.Error()
		return out
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	out.TotalBytes = total
	out.FreeBytes = free
	out.UsedBytes = total - free
	if total > 0 {
		out.UsedPct = int(out.UsedBytes * 100 / total)
	}
	return out
}
