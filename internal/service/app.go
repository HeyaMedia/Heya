package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/tailscale"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/watcher"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type App struct {
	config         *config.Config
	db             *pgxpool.Pool
	matcher        *matcher.Matcher
	downloader     *images.Downloader
	river          *river.Client[pgx.Tx]
	watcher        *watcher.Manager
	heya           *heyamedia.HeyaProvider
	transcoder     *transcoder.SessionManager
	transcodeCache *transcoder.CacheManager
	audioSessions  *transcoder.AudioSessionManager
	hub            *eventhub.Hub
	scheduler      *scheduler.Runner
	scanTask       *scheduler.ScanLibrariesTask
	tailscale      *tailscale.Server
	textSearcher   *sonicanalysis.TextSearcher
	modelFetcher   *sonicanalysis.ModelFetcher
	analyzer       *sonicanalysis.Analyzer
	envLibraries   map[int64]EnvManagedLibrary

	// Lifetime context cancelled by Close(). Used for fire-and-forget
	// goroutines (model fetches, tailscale Enable/Logout) that must outlive
	// the request that triggered them but should not survive shutdown.
	lifetimeCtx    context.Context
	lifetimeCancel context.CancelFunc
}

// Accessor methods for handler packages that need App internals.

func (a *App) SessionLookup() auth.SessionLookup               { return sqlc.New(a.db) }
func (a *App) TranscoderSessions() *transcoder.SessionManager  { return a.transcoder }
func (a *App) TranscoderCache() *transcoder.CacheManager       { return a.transcodeCache }
func (a *App) AudioSessions() *transcoder.AudioSessionManager  { return a.audioSessions }
func (a *App) EventHub() *eventhub.Hub                         { return a.hub }
func (a *App) ConfigSnapshot() *config.Config                  { return a.config }
func (a *App) WatcherManager() *watcher.Manager                { return a.watcher }
func (a *App) TaskScheduler() *scheduler.Runner                { return a.scheduler }
func (a *App) Metadata() *heyamedia.HeyaProvider               { return a.heya }
func (a *App) DBPool() *pgxpool.Pool                           { return a.db }
func (a *App) RiverClient() *river.Client[pgx.Tx]              { return a.river }
func (a *App) ScanLibrariesTask() *scheduler.ScanLibrariesTask { return a.scanTask }

func (a *App) SetScheduler(r *scheduler.Runner) { a.scheduler = r }

func (a *App) Tailscale() *tailscale.Server      { return a.tailscale }
func (a *App) SetTailscale(ts *tailscale.Server) { a.tailscale = ts }

func (a *App) TextSearcher() *sonicanalysis.TextSearcher { return a.textSearcher }
func (a *App) ModelFetcher() *sonicanalysis.ModelFetcher { return a.modelFetcher }
func (a *App) SonicAnalyzer() *sonicanalysis.Analyzer    { return a.analyzer }

// LifetimeContext returns a context cancelled when the App shuts down (via
// Close). Hand it to fire-and-forget goroutines that need to outlive the
// request that triggered them but should still terminate when the process
// is winding down.
func (a *App) LifetimeContext() context.Context { return a.lifetimeCtx }

// UpdateTailscaleConfig overlays the in-memory tailscale snapshot with new
// runtime values from a settings update. Used by the API handler after it
// persists to system_settings — keeps the snapshot in sync with DB without
// a re-Load. Env-sourced fields keep their provenance (the handler refuses
// writes to env-locked fields before getting here).
func (a *App) UpdateTailscaleConfig(enabled, https, funnel bool, hostname string) {
	if a.config.Tailscale.Enabled.Source != config.SourceEnv {
		a.config.Tailscale.Enabled = config.Field[bool]{Value: enabled, Source: config.SourceDB}
	}
	if a.config.Tailscale.HTTPS.Source != config.SourceEnv {
		a.config.Tailscale.HTTPS = config.Field[bool]{Value: https, Source: config.SourceDB}
	}
	if a.config.Tailscale.Funnel.Source != config.SourceEnv {
		a.config.Tailscale.Funnel = config.Field[bool]{Value: funnel, Source: config.SourceDB}
	}
	if a.config.Tailscale.Hostname.Source != config.SourceEnv && hostname != "" {
		a.config.Tailscale.Hostname = config.Field[string]{Value: hostname, Source: config.SourceDB}
	}
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	if err := AutoMigrate(cfg.DatabaseURL.Value); err != nil {
		return nil, err
	}

	db, err := database.Connect(ctx, cfg.DatabaseURL.Value)
	if err != nil {
		return nil, err
	}

	dl := images.NewDownloader(cfg.DataDir.Value)

	hm := heyamedia.NewClient(cfg.HeyaMediaURL.Value)
	heya := heyamedia.NewHeyaProvider(hm)

	log.Info().Str("url", cfg.HeyaMediaURL.Value).Msg("metadata provider registered via heya.media")

	hub := eventhub.New()

	m := matcher.New(db, matcher.DefaultOptions(), heya)

	var tc *transcoder.SessionManager
	var tcCache *transcoder.CacheManager
	var hwAccelProvider *transcoder.HwAccelProvider
	var audioSessions *transcoder.AudioSessionManager
	if transcoder.IsFFmpegAvailable() {
		tcCache = transcoder.NewCacheManager(cfg.TranscodeCacheDir.Value, cfg.TranscodeCacheMaxGB.Value)
		// Provider resolves on first transcode session, not at startup —
		// keeps service.New fork-free. See hwaccel_provider.go for the
		// Network.framework/atfork rationale.
		hwAccelProvider = transcoder.NewHwAccelProvider(cfg.DataDir.Value, cfg.HWAccel.Value)
		tc = transcoder.NewSessionManager(tcCache, hwAccelProvider, transcoder.NewFFmpegBuilder())
		audioSessions = transcoder.NewAudioSessionManager(tcCache)
	}

	riverClient, err := worker.Setup(ctx, worker.Config{
		DB:             db,
		DataDir:        cfg.DataDir.Value,
		HeyaMedia:      hm,
		Heya:           heya,
		Matcher:        m,
		Downloader:     dl,
		TranscodeCache: tcCache,
		HWAccel:        hwAccelProvider,
		Hub:            hub,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	scanTask := &scheduler.ScanLibrariesTask{DB: db, River: riverClient, Hub: hub}

	wm := watcher.NewManager(db, riverClient, func(libraryID int64, force bool) {
		scanTask.Enqueue(libraryID, force)
	})

	scanTask.Watcher = wm

	// Sonic-analysis pipeline. Always constructed so the API + CLI
	// can read state regardless of whether the scheduler is enabled.
	// ModelsDir is server-level (lives under DataDir). All other
	// knobs (accelerator, current_version, fetch-on-boot) come from
	// the system_settings row and are tweakable from the UI without
	// restarting the server. Models load lazily on first Analyze/
	// Embed call so a fresh boot with no models on disk doesn't
	// crash startup.
	modelsDir := cfg.DataDir.Value + "/models"
	bootSettings := DefaultSonicAnalysisSettings()
	saCfg := sonicanalysis.Config{
		ModelsDir:   modelsDir,
		Accelerator: sonicanalysis.Accelerator(bootSettings.Accelerator),
	}
	textSearcher := sonicanalysis.NewTextSearcher(saCfg)
	modelFetcher := sonicanalysis.NewModelFetcher(modelsDir, "")
	analyzer := sonicanalysis.NewAnalyzer(saCfg)

	// Lifetime context independent of the bootstrap ctx — bootstrap finishes
	// quickly but the App lives for the whole process. Cancelled by Close().
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())

	app := &App{
		config:         cfg,
		db:             db,
		matcher:        m,
		downloader:     dl,
		river:          riverClient,
		watcher:        wm,
		heya:           heya,
		transcoder:     tc,
		transcodeCache: tcCache,
		audioSessions:  audioSessions,
		hub:            hub,
		scanTask:       scanTask,
		textSearcher:   textSearcher,
		modelFetcher:   modelFetcher,
		analyzer:       analyzer,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
	}

	// Overlay persisted UI settings onto the config snapshot. Env-sourced
	// fields are preserved; only default-sourced fields get DB values.
	// Order matters less here since the two loaders touch disjoint fields.
	app.LoadTailscaleFromDB(ctx)
	app.LoadTranscoderFromDB(ctx)

	// Env bootstrap. Order: admin first (libraries.created_by FK requires
	// at least one user). Failures here are logged and continue — a misformed
	// env var shouldn't kill the server. The /api/config/sources response
	// will reflect whatever actually got persisted.
	if err := app.BootstrapAdminFromEnv(ctx); err != nil {
		log.Warn().Err(err).Msg("HEYA_ADMIN_* bootstrap failed")
	}
	if err := app.BootstrapLibrariesFromEnv(ctx); err != nil {
		log.Warn().Err(err).Msg("HEYA_LIBRARY_* bootstrap failed")
	}

	return app, nil
}

// StartSonicAnalysis kicks the model fetcher off in the background
// when sonic-analysis is enabled in the persisted settings. When
// disabled (the default), this is a no-op — no models, no analyzer.
// Flipping Enabled on from the Settings UI kicks off the fetch
// without needing a restart.
func (a *App) StartSonicAnalysis(ctx context.Context) {
	if a.modelFetcher == nil {
		return
	}
	settings := a.SonicAnalysisSettings(ctx)
	if !settings.Enabled {
		log.Info().Msg("sonicanalysis: disabled in settings (enable from Settings → Sonic Analysis)")
		return
	}
	go func() {
		if err := a.modelFetcher.Run(ctx); err != nil {
			log.Err(err).Msg("sonicanalysis: model fetcher exited with error")
		}
	}()
}

// SonicAnalysisEnabled is a cheap boolean accessor for callers that
// don't need the whole settings struct (e.g. the scheduler task's
// per-tick gate).
func (a *App) SonicAnalysisEnabled(ctx context.Context) bool {
	return a.SonicAnalysisSettings(ctx).Enabled
}

// ReconfigureSonicAnalysisAnalyzer rebuilds the Analyzer + TextSearcher
// with the current settings' accelerator choice. Caller must ensure
// the analyzer isn't mid-batch (state == Unloaded). Returns
// ErrSonicBusy if it is. The scheduler swallows this and waits for
// the next window naturally.
func (a *App) ReconfigureSonicAnalysisAnalyzer(ctx context.Context) error {
	if a.analyzer == nil {
		return nil
	}
	if a.analyzer.State() != sonicanalysis.StateUnloaded {
		return ErrSonicBusy
	}
	settings := a.SonicAnalysisSettings(ctx)
	modelsDir := a.config.DataDir.Value + "/models"
	saCfg := sonicanalysis.Config{
		ModelsDir:   modelsDir,
		Accelerator: sonicanalysis.Accelerator(settings.Accelerator),
	}
	a.analyzer = sonicanalysis.NewAnalyzer(saCfg)
	a.textSearcher.Close()
	a.textSearcher = sonicanalysis.NewTextSearcher(saCfg)
	return nil
}

// ErrSonicBusy is returned by ReconfigureSonicAnalysisAnalyzer when
// the analyzer is mid-batch; the caller should defer the rebuild
// until next idle.
var ErrSonicBusy = errors.New("sonicanalysis: analyzer is busy; settings will apply on next idle")

func (a *App) StartWorkers(ctx context.Context) error {
	return a.river.Start(ctx)
}

func (a *App) QueueCounts(ctx context.Context) (pending, running int) {
	row := a.db.QueryRow(ctx, "SELECT count(*) FILTER (WHERE state = 'available' OR state = 'retryable'), count(*) FILTER (WHERE state = 'running') FROM river_job")
	row.Scan(&pending, &running)
	return
}

func (a *App) EnqueuePendingFiles(ctx context.Context, libraryID int64) (int, error) {
	q := sqlc.New(a.db)
	files, err := q.ListLibraryFilesByStatus(ctx, sqlc.ListLibraryFilesByStatusParams{
		LibraryID: libraryID,
		Limit:     10000,
		Offset:    0,
		Status:    sqlc.FileStatusPending,
	})
	if err != nil {
		return 0, err
	}

	for _, f := range files {
		a.river.Insert(ctx, worker.ProcessFileArgs{
			LibraryFileID: f.ID,
			LibraryID:     libraryID,
			FilePath:      f.Path,
		}, nil)
	}

	return len(files), nil
}

func (a *App) StartWatchers(ctx context.Context) error {
	if err := a.watcher.StartAll(ctx); err != nil {
		return err
	}
	return nil
}

func (a *App) StopWorkers(ctx context.Context) error {
	if a.river == nil {
		return nil
	}
	// Graceful first: try to let in-flight jobs finish. If the context
	// times out, escalate to StopAndCancel which interrupts running
	// jobs so we don't leak River goroutines holding pool connections
	// (the cause of pgxpool.Close hangs we've seen under air reloads).
	stopErr := make(chan error, 1)
	go func() { stopErr <- a.river.Stop(ctx) }()
	select {
	case err := <-stopErr:
		return err
	case <-ctx.Done():
		hardCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return a.river.StopAndCancel(hardCtx)
	}
}

func (a *App) Close() {
	// Cancel first so any in-flight background goroutines unblock and
	// release resources before we tear down the pool / watcher.
	if a.lifetimeCancel != nil {
		a.lifetimeCancel()
	}
	if a.watcher != nil {
		a.watcher.StopAll()
	}
	if a.db != nil {
		a.db.Close()
	}
}
