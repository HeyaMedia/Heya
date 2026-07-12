package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Config holds every infrastructure-level knob loaded at boot. Each Field
// carries provenance so consumers (handlers, the UI) can tell whether a
// value came from env or fell back to the built-in default. Anything that
// belongs to the "user-tunable in the settings UI" category lives in DB
// tables, not here — see internal/service/sonicanalysis_settings.go and
// internal/service/tailscale_settings.go.
type Config struct {
	DatabaseURL      Field[string]
	DatabaseMaxConns Field[int]
	DatabaseMinConns Field[int]
	// PassiveMode turns the server into a read-mostly guest on its database:
	// no auto-migrate, no env bootstrap, no River workers, no filesystem
	// watchers, no scheduler tick loop, no sonic-analysis fetcher, no startup
	// orphan-rescue. It exists so local dev can point HEYA_DATABASE_URL at a
	// production DB to build UI against real data without the worker pool
	// stealing prod's queued jobs and scanning a /storage path that doesn't
	// exist locally (which would soft-delete the whole library). The HTTP API
	// and the read-only dashboard emitters still run. See docs/development.md.
	PassiveMode Field[bool]
	// AllowRemoteActive authorises ACTIVE mode (workers, watchers, scanner) to
	// run against a NON-local database. Defaults false: a source/dev checkout
	// pointed at a remote DB must stay PassiveMode=true, because active mode
	// against (e.g.) prod's DB turns this process into a second worker pool on
	// prod's queue and soft-deletes libraries when it scans paths that don't
	// exist locally. The deployed container image sets
	// HEYA_ALLOW_REMOTE_ACTIVE=true — it legitimately owns its remote DB. The
	// dev front-door (--dev-backend) can never satisfy it. Enforced in
	// cmd/heya/cmd/serve.go before any worker starts.
	AllowRemoteActive Field[bool]
	// ImageProxyURL is the base URL of another Heya instance whose image bytes
	// should be served in this one's place. Only consulted in passive mode,
	// where the local data dir holds none of the borrowed DB's images: the
	// public /api/.../image endpoints reverse-proxy the identical path to this
	// base (e.g. https://heya.example.ts.net) so posters/backdrops/covers
	// render. Empty → serve locally (images 404 in passive mode). See
	// docs/development.md.
	ImageProxyURL       Field[string]
	Host                Field[string]
	Port                Field[string]
	LogLevel            Field[string]
	LogFormat           Field[string]
	HeyaMediaURL        Field[string]
	DataDir             Field[string]
	HWAccel             Field[string]
	TranscodeCacheDir   Field[string]
	TranscodeCacheMaxGB Field[int]
	Tailscale           TailscaleConfig
	Jellyfin            JellyfinConfig
	Subsonic            SubsonicConfig
	Cast                CastConfig
	Jobs                JobsConfig
	// Podcast Index API credentials. Sign up at https://api.podcastindex.org
	// — free tier covers personal-use traffic comfortably. When empty the
	// /api/podcasts trending+search endpoints will surface a clear error.
	PodcastIndexKey    Field[string] `json:"-"` // never exposed via API
	PodcastIndexSecret Field[string] `json:"-"`
	// Last.fm application credentials (shared, server-level) — per-user session
	// keys live in user_music_services. Reads (history import) need only the
	// key; scrobbling needs both plus a user session.
	LastfmAPIKey Field[string] `json:"-"`
	LastfmSecret Field[string] `json:"-"`
}

// JellyfinConfig gates the Jellyfin-compatible API surface (internal/jellyfin)
// — a second route tree (/System/*, /Users/*, /Items/*, /socket, /emby/*) that
// lets stock Jellyfin clients (Infuse, Finamp, Streamyfin, jellyfin-web...)
// talk to Heya as if it were a Jellyfin server. Enabled follows the standard
// env > db > default merge: settable from the UI, locked when the env var is
// present. The routes are always mounted; the flag is checked per-request, so
// UI flips take effect without a restart.
type JellyfinConfig struct {
	Enabled Field[bool]
}

// SubsonicConfig gates the Subsonic/OpenSubsonic-compatible API surface
// (internal/subsonic) mounted under /subsonic — lets stock Subsonic music
// clients (Symfonium, DSub, play:Sub, Tempo, Supersonic...) browse and
// stream Heya's music libraries. Same provenance semantics as Jellyfin:
// env > db > default, checked per-request so UI flips need no restart.
type SubsonicConfig struct {
	Enabled Field[bool]
}

// CastConfig gates server-side casting (internal/cast): mDNS discovery
// of network receivers plus the playback sessions that stream to them.
// Default on — discovery is a passive mDNS browse; nothing plays until
// a user starts a session. Same env > db > default provenance as
// Jellyfin/Subsonic, checked live so UI flips need no restart.
type CastConfig struct {
	Enabled Field[bool]
}

type JobsConfig struct {
	Workers map[string]Field[int]
}

// TailscaleConfig holds the env-sourced tailscale knobs. Enabled/HTTPS/Funnel
// can also be set from the UI (DB-backed) — the effective value is computed
// at request time by merging env > db > default. AuthKey and StateDir are
// boot-time only: never UI-editable, never persisted to DB.
type TailscaleConfig struct {
	Enabled  Field[bool]
	Hostname Field[string]
	AuthKey  Field[string] `json:"-"` // never exposed via API
	StateDir Field[string]
	HTTPS    Field[bool]
	Funnel   Field[bool]
}

// Load reads .env / .env.local (without overriding real env), then resolves
// every Field from the environment. Defaults are applied for any var that
// wasn't set. There is no yaml layer — Heya is env-only.
func Load() *Config {
	loadDotEnv()

	dataDir := envString("HEYA_DATA_DIR", "./data")

	return &Config{
		DatabaseURL:         envString("HEYA_DATABASE_URL", "postgres://heya:heya@localhost:5440/heya?sslmode=disable"),
		DatabaseMaxConns:    envInt("HEYA_DB_MAX_CONNS", 30),
		DatabaseMinConns:    envInt("HEYA_DB_MIN_CONNS", 2),
		PassiveMode:         envBool("HEYA_PASSIVE_MODE", false),
		AllowRemoteActive:   envBool("HEYA_ALLOW_REMOTE_ACTIVE", false),
		ImageProxyURL:       envString("HEYA_IMAGE_PROXY_URL", ""),
		Host:                envString("HEYA_HOST", "0.0.0.0"),
		Port:                envString("HEYA_PORT", "8080"),
		LogLevel:            envString("HEYA_LOG_LEVEL", "info"),
		LogFormat:           envString("HEYA_LOG_FORMAT", "console"),
		HeyaMediaURL:        envString("HEYA_MEDIA_URL", "https://heya.media"),
		DataDir:             dataDir,
		HWAccel:             envString("HEYA_HWACCEL", "auto"),
		TranscodeCacheDir:   envString("HEYA_TRANSCODE_CACHE_DIR", dataDir.Value+"/transcode"),
		TranscodeCacheMaxGB: envInt("HEYA_TRANSCODE_CACHE_MAX_GB", 50),
		PodcastIndexKey:     envString("HEYA_PODCAST_INDEX_KEY", ""),
		PodcastIndexSecret:  envString("HEYA_PODCAST_INDEX_SECRET", ""),
		LastfmAPIKey:        envString("HEYA_LASTFM_API_KEY", ""),
		LastfmSecret:        envString("HEYA_LASTFM_SECRET", ""),
		Jellyfin: JellyfinConfig{
			Enabled: envBool("HEYA_JELLYFIN_API_ENABLED", false),
		},
		Subsonic: SubsonicConfig{
			Enabled: envBool("HEYA_SUBSONIC_API_ENABLED", false),
		},
		Cast: CastConfig{
			Enabled: envBool("HEYA_CAST_ENABLED", true),
		},
		Jobs: JobsConfig{
			Workers: loadJobWorkerFields(),
		},
		Tailscale: TailscaleConfig{
			Enabled:  envBool("HEYA_TAILSCALE_ENABLED", false),
			Hostname: envString("HEYA_TAILSCALE_HOSTNAME", "heya"),
			AuthKey:  envString("HEYA_TAILSCALE_AUTHKEY", ""),
			StateDir: envString("HEYA_TAILSCALE_STATE_DIR", dataDir.Value+"/tailscale"),
			HTTPS:    envBool("HEYA_TAILSCALE_HTTPS", false),
			Funnel:   envBool("HEYA_TAILSCALE_FUNNEL", false),
		},
	}
}

var DefaultJobWorkerCounts = map[string]int{
	"kickoff_library_scan":      1,
	"process_scan":              4,
	"fetch_metadata":            4,
	"apply_metadata":            4,
	"ffprobe":                   1,
	"detect_local_assets":       1,
	"enrich_media_item":         1,
	"person_fetch":              8,
	"ratings_fetch":             4,
	"force_refresh_metadata":    1,
	"fetch_artwork":             4,
	"download_image":            4,
	"save_images":               1,
	"force_refresh_images":      1,
	"save_nfo":                  1,
	"save_music_nfo":            1,
	"scan_track_loudness":       1,
	"scan_album_loudness":       1,
	"scan_track_fingerprint":    1,
	"scan_media_segments_file":  8,
	"scan_keyframes":            1,
	"detect_segments_season":    1,
	"detect_segments_movie":     1,
	"trickplay":                 1,
	"thumbnails":                1,
	"sonic_analysis":            1,
	"transcode":                 1,
	"artist_centroid":           1,
	"album_centroid":            1,
	"scan_library_disk":         1,
	"kickoff_refresh_stale":     1,
	"kickoff_music_loudness":    1,
	"kickoff_music_fingerprint": 1,
	"kickoff_media_segments":    1,
	"kickoff_detect_segments":   1,
	"kickoff_trickplay":         1,
	"kickoff_thumbnails":        1,
	"kickoff_sonic_analysis":    1,
	"soft_delete":               1,
	"debounce_sweep":            1,
	"default":                   1,
}

func JobWorkerKinds() []string {
	kinds := make([]string, 0, len(DefaultJobWorkerCounts))
	for kind := range DefaultJobWorkerCounts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func JobWorkerEnvVar(kind string) string {
	name := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(kind))
	return "HEYA_JOB_WORKERS_" + name
}

func loadJobWorkerFields() map[string]Field[int] {
	out := make(map[string]Field[int], len(DefaultJobWorkerCounts))
	for kind, def := range DefaultJobWorkerCounts {
		field := envInt(JobWorkerEnvVar(kind), def)
		if field.Value < 1 {
			field.Value = def
		}
		out[kind] = field
	}
	return out
}

func (c *Config) JobWorkerCounts() map[string]int {
	out := make(map[string]int, len(DefaultJobWorkerCounts))
	for kind, def := range DefaultJobWorkerCounts {
		value := def
		if c != nil && c.Jobs.Workers != nil {
			if field, ok := c.Jobs.Workers[kind]; ok && field.Value > 0 {
				value = field.Value
			}
		}
		out[kind] = value
	}
	return out
}

func loadDotEnv() {
	realEnv := map[string]bool{}
	for _, entry := range os.Environ() {
		k, _, ok := strings.Cut(entry, "=")
		if ok {
			realEnv[k] = true
		}
	}
	values := map[string]string{}
	for _, path := range []string{".env", ".env.local"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if len(v) >= 2 {
				first, last := v[0], v[len(v)-1]
				if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
					v = v[1 : len(v)-1]
				}
			}
			values[k] = v
		}
	}
	for k, v := range values {
		if !realEnv[k] {
			_ = os.Setenv(k, v)
		}
	}
}

// Addr returns the host:port the HTTP server should bind.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host.Value, c.Port.Value)
}

// NOTE: the active-mode "is this DB local?" check intentionally lives in
// internal/database (database.AllHostsLocal), not here — it must classify the
// host pgx ACTUALLY dials (?host= / DSN / PGHOST / fallbacks), which only pgx's
// own parser resolves. A net/url parse here would be bypassable.

type sourceField struct {
	key   string
	entry func(*Config) SourceEntry
}

var sourceFields = []sourceField{
	{"infra.database_url", func(c *Config) SourceEntry { return c.DatabaseURL.Entry() }},
	{"infra.database_max_conns", func(c *Config) SourceEntry { return c.DatabaseMaxConns.Entry() }},
	{"infra.database_min_conns", func(c *Config) SourceEntry { return c.DatabaseMinConns.Entry() }},
	{"infra.passive_mode", func(c *Config) SourceEntry { return c.PassiveMode.Entry() }},
	{"infra.allow_remote_active", func(c *Config) SourceEntry { return c.AllowRemoteActive.Entry() }},
	{"infra.image_proxy_url", func(c *Config) SourceEntry { return c.ImageProxyURL.Entry() }},
	{"infra.host", func(c *Config) SourceEntry { return c.Host.Entry() }},
	{"infra.port", func(c *Config) SourceEntry { return c.Port.Entry() }},
	{"infra.log_level", func(c *Config) SourceEntry { return c.LogLevel.Entry() }},
	{"infra.log_format", func(c *Config) SourceEntry { return c.LogFormat.Entry() }},
	{"infra.data_dir", func(c *Config) SourceEntry { return c.DataDir.Entry() }},
	{"infra.heya_media_url", func(c *Config) SourceEntry { return c.HeyaMediaURL.Entry() }},
	{"transcoder.hwaccel", func(c *Config) SourceEntry { return c.HWAccel.Entry() }},
	{"transcoder.cache_dir", func(c *Config) SourceEntry { return c.TranscodeCacheDir.Entry() }},
	{"transcoder.cache_max_gb", func(c *Config) SourceEntry { return c.TranscodeCacheMaxGB.Entry() }},
	{"jellyfin.enabled", func(c *Config) SourceEntry { return c.Jellyfin.Enabled.Entry() }},
	{"subsonic.enabled", func(c *Config) SourceEntry { return c.Subsonic.Enabled.Entry() }},
	{"cast.enabled", func(c *Config) SourceEntry { return c.Cast.Enabled.Entry() }},
	{"tailscale.enabled", func(c *Config) SourceEntry { return c.Tailscale.Enabled.Entry() }},
	{"tailscale.hostname", func(c *Config) SourceEntry { return c.Tailscale.Hostname.Entry() }},
	{"tailscale.state_dir", func(c *Config) SourceEntry { return c.Tailscale.StateDir.Entry() }},
	{"tailscale.https", func(c *Config) SourceEntry { return c.Tailscale.HTTPS.Entry() }},
	{"tailscale.funnel", func(c *Config) SourceEntry { return c.Tailscale.Funnel.Entry() }},
}

// Sources returns the flat key→provenance map for the infra layer. The
// /api/config/sources endpoint extends this with DB-backed setting groups
// (tailscale.*, sonic_analysis.*, library.N.*).
func (c *Config) Sources() map[string]SourceEntry {
	out := make(map[string]SourceEntry, len(sourceFields)+len(DefaultJobWorkerCounts))
	for _, field := range sourceFields {
		out[field.key] = field.entry(c)
	}
	for _, kind := range JobWorkerKinds() {
		if c.Jobs.Workers == nil {
			continue
		}
		if field, ok := c.Jobs.Workers[kind]; ok {
			out["jobs.workers."+kind] = field.Entry()
		}
	}
	return out
}
