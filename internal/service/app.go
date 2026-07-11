package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/imageserve"
	"github.com/karbowiak/heya/internal/llm"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/podcastindex"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/radiobrowser"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/sessions"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/tailscale"
	"github.com/karbowiak/heya/internal/textembed"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/watcher"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
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
	scheduler      *scheduler.Trigger
	tailscale      tailscale.Manager
	textSearcher   *sonicanalysis.TextSearcher
	modelFetcher   *sonicanalysis.ModelFetcher
	analyzer       *sonicanalysis.Analyzer
	sonicHolder    *sonicanalysis.Holder

	// Optional embedding recommendation engine (HEYA_RECOMMENDATIONS_ML_ENABLED).
	// recEmbedder is lazy-loaded on first use when enabled; recModelsDir holds the
	// BGE model files the recFetcher downloads.
	recFetcher    *sonicanalysis.ModelFetcher
	recEmbedder   *textembed.Embedder
	recEmbedderMu sync.Mutex
	recModelsDir  string
	imageResizer  *imageserve.Resizer
	imageFetch    singleflight.Group // coalesces concurrent on-demand image fetches by cache key
	keyframeScan  singleflight.Group // coalesces asynchronous playback-triggered keyframe analysis
	waveformScan  singleflight.Group // coalesces playback/UI-triggered waveform generation
	envLibraries  map[int64]EnvManagedLibrary
	radioBrowser  *radiobrowser.Client
	podcastIndex  *podcastindex.Client
	sessions      *sessions.Store
	llmLocal      *llm.LocalRuntime
	castMgr       *cast.Manager

	// Lifetime context cancelled by Close(). Used for fire-and-forget
	// goroutines (model fetches, tailscale Enable/Logout) that must outlive
	// the request that triggered them but should not survive shutdown.
	lifetimeCtx    context.Context
	lifetimeCancel context.CancelFunc

	// Wall-clock start time, captured once in New. Drives the uptime metric
	// surfaced via /api/admin/system.
	startedAt time.Time

	// TTL cache for the dashboard missing_count — the three-bucket anti-join
	// costs ~750ms at prod scale and only changes on scan/cleanup, so the
	// dashboard shouldn't recompute it per render. Guarded by missingCountMu.
	missingCountMu sync.Mutex
	missingCount   int
	missingCountAt time.Time
}

// StartedAt returns the wall-clock time at which the App was constructed.
func (a *App) StartedAt() time.Time { return a.startedAt }

// Accessor methods for handler packages that need App internals.

func (a *App) SessionLookup() auth.SessionLookup              { return sqlc.New(a.db) }
func (a *App) TranscoderSessions() *transcoder.SessionManager { return a.transcoder }
func (a *App) TranscoderCache() *transcoder.CacheManager      { return a.transcodeCache }
func (a *App) AudioSessions() *transcoder.AudioSessionManager { return a.audioSessions }
func (a *App) EventHub() *eventhub.Hub                        { return a.hub }
func (a *App) ConfigSnapshot() *config.Config                 { return a.config }
func (a *App) WatcherManager() *watcher.Manager               { return a.watcher }
func (a *App) TaskScheduler() *scheduler.Trigger              { return a.scheduler }
func (a *App) Metadata() *heyamedia.HeyaProvider              { return a.heya }
func (a *App) DBPool() *pgxpool.Pool                          { return a.db }
func (a *App) RiverClient() *river.Client[pgx.Tx]             { return a.river }
func (a *App) Sessions() *sessions.Store                      { return a.sessions }

func (a *App) SetScheduler(s *scheduler.Trigger) { a.scheduler = s }

func (a *App) Tailscale() tailscale.Manager      { return a.tailscale }
func (a *App) SetTailscale(ts tailscale.Manager) { a.tailscale = ts }

func (a *App) TextSearcher() *sonicanalysis.TextSearcher { return a.textSearcher }
func (a *App) ModelFetcher() *sonicanalysis.ModelFetcher { return a.modelFetcher }
func (a *App) SonicAnalyzer() *sonicanalysis.Analyzer    { return a.analyzer }
func (a *App) SonicHolder() *sonicanalysis.Holder        { return a.sonicHolder }
func (a *App) ImageResizer() *imageserve.Resizer         { return a.imageResizer }

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
	// Passive mode is a read-mostly guest on someone else's DB (typically a
	// dev box pointed at production). Skip AutoMigrate so we never mutate the
	// target's schema to match this binary's branch — if the schemas differ,
	// failing local queries are a far safer outcome than altering prod.
	if !cfg.PassiveMode.Value {
		if err := AutoMigrate(cfg.DatabaseURL.Value); err != nil {
			return nil, err
		}
	} else {
		log.Warn().Msg("passive mode: skipping auto-migrate; this binary will NOT alter the target schema")
	}

	db, err := database.ConnectWithOptions(ctx, cfg.DatabaseURL.Value, database.Options{
		MaxConns: int32(cfg.DatabaseMaxConns.Value),
		MinConns: int32(cfg.DatabaseMinConns.Value),
	})
	if err != nil {
		return nil, err
	}

	dl := images.NewDownloader(cfg.DataDir.Value)

	hm := heyamedia.NewClient(cfg.HeyaMediaURL.Value)
	heya := heyamedia.NewHeyaProvider(hm)

	log.Info().Str("url", cfg.HeyaMediaURL.Value).Msg("metadata provider registered via heya.media")

	hub := eventhub.New()

	LoadJobWorkersFromDB(ctx, db, cfg)

	m := matcher.New(db, matcher.DefaultOptions(), heya, worker.ProbeFile)

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

	// Sonic-analysis Holder is built before worker.Setup so we can hand
	// it to the analyze_track_facets worker. The Holder owns the full
	// analysis bundle — Discogs specialized heads, EffNet base,
	// classifier heads, and the CLAP audio encoder (~hundreds of MB
	// resident together). Idle-unload kicks in 5 min after the last
	// lease releases, returning all the model memory at once. The CLAP
	// *text* encoder lives separately in TextSearcher since it serves
	// the search-sonic text-prompt endpoint, not per-track analysis.
	modelsDir := cfg.DataDir.Value + "/models"
	bootSettings := DefaultSonicAnalysisSettings()
	saCfg := sonicanalysis.Config{
		ModelsDir:   modelsDir,
		Accelerator: sonicanalysis.Accelerator(bootSettings.Accelerator),
	}
	sonicHolder := sonicanalysis.NewHolder(saCfg, 5*time.Minute)

	// Watcher hook + sonic enabled gate are both built in two passes:
	// worker.Setup wires the kickoff workers with indirection wrappers,
	// then after the App is fully constructed we assign the concrete
	// targets. We can't do that directly because workers can't be
	// re-wired after AddWorker — see lazyWatcher / lazyEnabled below.
	var watcherPauser worker.WatcherPauser        // assigned after watcher.NewManager
	var sonicEnabledFn func(context.Context) bool // assigned after App construction
	var embedBackfillFn worker.EmbedBackfillFn    // assigned after App construction

	progress := worker.NewTaskProgressBroadcaster(hub)

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
		SonicHolder:    sonicHolder,
		SonicEnabled: func(ctx context.Context) bool {
			if sonicEnabledFn == nil {
				return false
			}
			return sonicEnabledFn(ctx)
		},
		EmbedBackfill: func(ctx context.Context, force bool) (int, int, error) {
			if embedBackfillFn == nil {
				return 0, 0, nil
			}
			return embedBackfillFn(ctx, force)
		},
		Watcher:      lazyWatcher{ptr: &watcherPauser},
		Progress:     progress,
		Passive:      cfg.PassiveMode.Value,
		WorkerCounts: cfg.JobWorkerCounts(),
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	wm := watcher.NewManager(db, riverClient, func(libraryID int64, force bool) {
		_, _ = riverClient.Insert(ctx, worker.KickoffLibraryScanArgs{LibraryID: libraryID, Force: force}, nil)
	})
	watcherPauser = wm

	// Reverse-wire SonicEnabled now that we'll have an App with the
	// system_settings accessor. Assignment happens before any worker
	// fires (StartWorkers is called later from cmd/serve), so the
	// kickoff sees a fully-initialised closure on first invocation.
	_ = sonicEnabledFn // keep variable address stable while we capture it; assigned below

	// Auxiliary sonic-analysis surfaces — text-only search + the model
	// fetcher used by the Settings page. The Holder + Analyzer above
	// own the per-track analysis path. saCfg + modelsDir were
	// initialised in the worker.Setup block; reuse them here.
	textSearcher := sonicanalysis.NewTextSearcher(saCfg)
	modelFetcher := sonicanalysis.NewModelFetcher(modelsDir, "")
	analyzer := sonicanalysis.NewAnalyzer(saCfg)

	// Optional embedding recommendation engine — its own model dir + fetcher
	// (BGE-large-en), lazy embedder loaded on first use when enabled.
	recModelsDir := cfg.DataDir.Value + "/models/recommendations"
	recFetcher := sonicanalysis.NewModelFetcherWithManifest(recModelsDir, "", recommendationsMLManifest())

	resizer, err := imageserve.New(cfg.DataDir.Value + "/images/resized")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init image resizer: %w", err)
	}

	// Lifetime context independent of the bootstrap ctx — bootstrap finishes
	// quickly but the App lives for the whole process. Cancelled by Close().
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())

	// Managed llama-server runtime for the AI subsystem (mode=local). Spawns
	// nothing until first use; artifacts live under DataDir/llm.
	llmLocal := llm.NewLocalRuntime(cfg.DataDir.Value + "/llm")
	llmLocal.Bind(lifetimeCtx)

	// Server-side cast manager. Constructed always (accessors stay
	// nil-safe); discovery only starts via StartCast when cast.enabled.
	castMgr := cast.New(cfg.DataDir.Value)
	castMgr.SetHub(hub)

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
		textSearcher:   textSearcher,
		modelFetcher:   modelFetcher,
		analyzer:       analyzer,
		sonicHolder:    sonicHolder,
		recFetcher:     recFetcher,
		recModelsDir:   recModelsDir,
		imageResizer:   resizer,
		radioBrowser:   radiobrowser.New(),
		podcastIndex:   podcastindex.New(cfg.PodcastIndexKey.Value, cfg.PodcastIndexSecret.Value),
		sessions:       sessions.New(lifetimeCtx, hub),
		llmLocal:       llmLocal,
		castMgr:        castMgr,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
		startedAt:      time.Now(),
	}

	// Wire the SonicEnabled closure now that the App can answer the
	// system_settings query. Worker.Setup captured a closure pointing
	// at sonicEnabledFn; assigning here makes that closure live before
	// any kickoff fires.
	sonicEnabledFn = app.SonicAnalysisEnabled
	embedBackfillFn = app.backfillEmbeddingsForTask

	// Cast scrobbles route through the same RecordPlayback dispatch the
	// HTTP endpoint uses; wired post-construction like SonicEnabled.
	castMgr.SetPlaybackSink(app.castPlaybackSink)

	// Overlay persisted UI settings onto the config snapshot. Env-sourced
	// fields are preserved; only default-sourced fields get DB values.
	// Order matters less here since the two loaders touch disjoint fields.
	//
	// Tailscale is deliberately NOT loaded from the DB in passive mode: the
	// borrowed (production) DB has tailscale.enabled=true with hostname `heya`,
	// and overlaying that would make serve.go bring up a tsnet node on this dev
	// box that joins the tailnet under prod's identity — a node-name collision
	// with the real server. In passive mode tailscale stays env-only; a dev who
	// wants it sets HEYA_TAILSCALE_ENABLED with a distinct HEYA_TAILSCALE_HOSTNAME.
	if !cfg.PassiveMode.Value {
		app.LoadTailscaleFromDB(ctx)
	}
	app.LoadTranscoderFromDB(ctx)

	// Env bootstrap. Order: admin first (libraries.created_by FK requires
	// at least one user). Failures here are logged and continue — a misformed
	// env var shouldn't kill the server. The /api/config/sources response
	// will reflect whatever actually got persisted. Skipped in passive mode:
	// a dev box's local HEYA_ADMIN_* / HEYA_LIBRARY_* must never overwrite the
	// users and library paths of the production DB it's borrowing.
	if !cfg.PassiveMode.Value {
		if err := app.BootstrapAdminFromEnv(ctx); err != nil {
			log.Warn().Err(err).Msg("HEYA_ADMIN_* bootstrap failed")
		}
		if err := app.BootstrapLibrariesFromEnv(ctx); err != nil {
			log.Warn().Err(err).Msg("HEYA_LIBRARY_* bootstrap failed")
		}
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

// ReconfigureSonicAnalysisAnalyzer applies the current settings'
// accelerator choice to every sonic-analysis surface: the worker-facing
// Holder (which owns its own Analyzer built from its own cfg copy — the
// long-standing gap: Holder.Reconfigure previously had no callers, so
// the analysis worker kept the old accelerator until restart), the
// facets-read Analyzer, and the TextSearcher.
//
// All three are reconfigured IN PLACE (the Holder under its lease lock;
// Analyzer.Reconfigure CASes the state machine; TextSearcher.Reconfigure
// closes its session under the same lock Embed holds). The old version
// swapped the a.analyzer / a.textSearcher pointers instead — an
// unsynchronized write racing every concurrent reader, and Close() on
// the old searcher could destroy the ONNX session under an in-flight
// Embed (native use-after-free).
//
// Busy handling: a mid-batch Holder stashes the config and applies it
// itself when the current leases drain (no user action needed), but
// ErrSonicBusy is still returned so the UI can show "applies when the
// current batch finishes". The standalone analyzer/searcher can't be
// busy in the server (nothing loads them outside the CLI); a re-save
// retries idempotently regardless.
func (a *App) ReconfigureSonicAnalysisAnalyzer(ctx context.Context) error {
	if a.analyzer == nil {
		return nil
	}
	settings := a.SonicAnalysisSettings(ctx)
	modelsDir := a.config.DataDir.Value + "/models"
	saCfg := sonicanalysis.Config{
		ModelsDir:   modelsDir,
		Accelerator: sonicanalysis.Accelerator(settings.Accelerator),
	}
	busy := false
	if a.sonicHolder != nil {
		if err := a.sonicHolder.Reconfigure(saCfg); err != nil {
			busy = true
		}
	}
	if err := a.analyzer.Reconfigure(saCfg); err != nil {
		busy = true
	}
	a.textSearcher.Reconfigure(saCfg)
	if busy {
		return ErrSonicBusy
	}
	return nil
}

// ErrSonicBusy is returned by ReconfigureSonicAnalysisAnalyzer when
// the analyzer is mid-batch; the caller should defer the rebuild
// until next idle.
var ErrSonicBusy = errors.New("sonicanalysis: analyzer is busy; settings will apply on next idle")

// lazyWatcher resolves a watcher.Manager indirectly so the
// KickoffLibraryScanWorker (constructed inside worker.Setup) can
// pause/resume the watcher (constructed afterwards). Pause/Resume on
// the zero-value indirection are no-ops, which is the correct
// behaviour during the bootstrap window before the watcher exists.
type lazyWatcher struct {
	ptr *worker.WatcherPauser
}

func (l lazyWatcher) Pause(libraryID int64) {
	if l.ptr == nil || *l.ptr == nil {
		return
	}
	(*l.ptr).Pause(libraryID)
}

func (l lazyWatcher) Resume(libraryID int64) {
	if l.ptr == nil || *l.ptr == nil {
		return
	}
	(*l.ptr).Resume(libraryID)
}

func (a *App) StartWorkers(ctx context.Context) error {
	return a.river.Start(ctx)
}

func (a *App) QueueCounts(ctx context.Context) (pending, running int) {
	if counts, err := queueops.CountActive(ctx, a.db); err == nil {
		pending = counts.Pending
		running = counts.Running
	}
	return
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
	// Cast sessions first, before lifetimeCancel SIGTERMs the child
	// processes out from under them — receivers should see the graceful
	// ACTION=STOP → TEARDOWN path, not a torn connection.
	if a.castMgr != nil {
		a.castMgr.Stop()
	}
	// Cancel so any in-flight background goroutines unblock and
	// release resources before we tear down the pool / watcher.
	if a.lifetimeCancel != nil {
		a.lifetimeCancel()
	}
	if a.llmLocal != nil {
		// lifetimeCancel above already signalled the subprocess; Stop waits
		// for it to actually exit so we never leak a llama-server.
		a.llmLocal.Stop()
	}
	if a.watcher != nil {
		a.watcher.StopAll()
	}
	if a.transcoder != nil {
		a.transcoder.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}
