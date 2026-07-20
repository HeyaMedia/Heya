package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/acoustid"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/cast"
	"github.com/karbowiak/heya/internal/communitysegments"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/diagnostics"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/imagegen"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/imageserve"
	"github.com/karbowiak/heya/internal/ingress"
	"github.com/karbowiak/heya/internal/llm"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/mediaanalysis"
	"github.com/karbowiak/heya/internal/mediaprobe"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/playbackgrant"
	"github.com/karbowiak/heya/internal/playlistsync"
	"github.com/karbowiak/heya/internal/podcastindex"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/radiobrowser"
	"github.com/karbowiak/heya/internal/remote"
	"github.com/karbowiak/heya/internal/runtimelease"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/securityevents"
	"github.com/karbowiak/heya/internal/sessions"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/tailscale"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/watcher"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

type leaseCloser interface {
	Close() error
}

var errAppClosing = errors.New("application is closing")

type App struct {
	config                    *config.Config
	configMu                  sync.RWMutex
	db                        *pgxpool.Pool
	diagnostics               *diagnostics.Collector
	sessionLookup             *auth.AsyncSessionLookup
	loginGuardMu              sync.Mutex
	loginGuard                *auth.LoginGuard
	securityEventsMu          sync.Mutex
	securityEvents            *securityevents.Recorder
	trustedNetworksSettingsMu sync.Mutex
	coordinatorLease          leaseCloser
	matcher                   *matcher.Matcher
	downloader                *images.Downloader
	river                     *river.Client[pgx.Tx]
	riverMu                   sync.Mutex
	riverStarted              bool
	closing                   bool
	watcher                   *watcher.Manager
	heya                      *heyametadata.HeyaProvider
	transcoder                *transcoder.SessionManager
	transcodeCache            *transcoder.CacheManager
	audioSessions             *transcoder.AudioSessionManager
	hub                       *eventhub.Hub
	relayPublisher            *eventhub.RelayPublisher
	scheduler                 *scheduler.Trigger
	networkMu                 sync.RWMutex
	tailscale                 tailscale.Manager
	remote                    *remote.Manager
	ingress                   *ingress.Manager
	tailscaleSettingsMu       sync.Mutex
	remoteSettingsMu          sync.Mutex
	tailscaleTransition       backgroundTransition
	remoteTransition          backgroundTransition
	textSearcher              *sonicanalysis.TextSearcher
	modelFetcher              *sonicanalysis.ModelFetcher
	analyzer                  *sonicanalysis.Analyzer
	sonicHolder               *sonicanalysis.Holder
	taskProgress              *worker.TaskProgressBroadcaster

	// Optional embedding recommendation engine (HEYA_RECOMMENDATIONS_ML_ENABLED).
	// recEmbedder is lazy-loaded on first use when enabled; recModelsDir holds the
	// BGE-M3 model files the recFetcher downloads.
	recFetcher *sonicanalysis.ModelFetcher
	// recEmbedder is generation-owned: settings changes retire the current
	// generation immediately for new callers, while outstanding leases keep its
	// native ONNX session alive until their inference completes.
	recEmbedder        *recEmbedderGeneration
	recEmbedderMu      sync.Mutex
	recModelsDir       string
	imageResizer       *imageserve.Resizer
	imageFetch         singleflight.Group // coalesces concurrent on-demand image fetches by cache key
	mediaAssetUploadMu sync.Mutex         // serializes file+DB replacement across image extensions
	keyframeRequests   singleflight.Group // coalesces on-demand keyframe orchestration and queue handoff
	waveformScan       singleflight.Group // coalesces playback/UI-triggered waveform generation
	mediaAnalysis      *mediaanalysis.Service
	envLibraries       map[int64]EnvManagedLibrary
	radioBrowser       *radiobrowser.Client
	podcastIndex       *podcastindex.Client
	sessions           *sessions.Store
	llmLocal           *llm.LocalRuntime
	imageRuntime       *imagegen.Runtime
	castMgr            *cast.Manager
	playbackGrants     *playbackgrant.Manager
	castSettingsMu     sync.Mutex
	castAccessMu       sync.RWMutex
	// castAllowedUsers is the explicit non-admin allowlist loaded from
	// system_settings. Admins are always allowed and are intentionally not
	// required to appear here, so an edit cannot lock every admin out of the
	// recovery/configuration path.
	castAllowedUsers map[int64]struct{}

	// Lifetime context cancelled by Close(). Used for fire-and-forget
	// goroutines (model fetches, tailscale Enable/Logout) that must outlive
	// the request that triggered them but should not survive shutdown.
	lifetimeCtx      context.Context
	lifetimeCancel   context.CancelFunc
	backgroundMu     sync.Mutex
	backgroundClosed bool
	backgroundWG     sync.WaitGroup
	closeOnce        sync.Once

	// Wall-clock start time, captured once in New. Drives the uptime metric
	// surfaced via /api/admin/system.
	startedAt time.Time

	// Test seam: substitutes the credential-backed playlist provider so sync
	// flows can run against a fake without network access. Nil in production.
	playlistProviderOverride func(userID int64, service string) playlistsync.Provider

	// TTL cache for the dashboard missing_count — the three-bucket anti-join
	// costs ~750ms at prod scale and only changes on scan/cleanup, so the
	// dashboard shouldn't recompute it per render. Guarded by missingCountMu.
	missingCountMu sync.Mutex
	missingCount   int
	missingCountAt time.Time

	// The Jobs page used to run three independent counts over river_job on
	// every refresh. At large backlogs those were some of the most expensive
	// queries in the application. Keep one grouped snapshot and derive all
	// three API responses from it for a short period.
	jobCountsMu sync.Mutex
	jobCounts   []jobCountRow
	jobCountsAt time.Time
}

// StartedAt returns the wall-clock time at which the App was constructed.
func (a *App) StartedAt() time.Time { return a.startedAt }

// Accessor methods for handler packages that need App internals.

func (a *App) SessionLookup() auth.SessionLookup {
	if a != nil && a.sessionLookup != nil {
		return a.sessionLookup
	}
	return sqlc.New(a.db)
}

func (a *App) LoginGuard() *auth.LoginGuard {
	if a == nil {
		return auth.NewLoginGuard()
	}
	a.loginGuardMu.Lock()
	defer a.loginGuardMu.Unlock()
	if a.loginGuard == nil {
		a.loginGuard = auth.NewLoginGuard()
	}
	return a.loginGuard
}
func (a *App) SecurityEvents() *securityevents.Recorder {
	if a == nil {
		return securityevents.New(1)
	}
	a.securityEventsMu.Lock()
	defer a.securityEventsMu.Unlock()
	if a.securityEvents == nil {
		a.securityEvents = securityevents.New(200)
	}
	return a.securityEvents
}
func (a *App) TranscoderSessions() *transcoder.SessionManager { return a.transcoder }
func (a *App) TranscoderCache() *transcoder.CacheManager      { return a.transcodeCache }
func (a *App) AudioSessions() *transcoder.AudioSessionManager { return a.audioSessions }
func (a *App) EventHub() *eventhub.Hub                        { return a.hub }

// ConfigSnapshot returns an immutable-by-convention deep copy of the current
// effective runtime config. In particular, callers never share the mutable
// job-worker map with App.
func (a *App) ConfigSnapshot() *config.Config {
	if a == nil {
		return nil
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	if a.config == nil {
		return nil
	}
	snapshot := *a.config
	if a.config.Jobs.Workers != nil {
		snapshot.Jobs.Workers = make(map[string]config.Field[int], len(a.config.Jobs.Workers))
		for kind, field := range a.config.Jobs.Workers {
			snapshot.Jobs.Workers[kind] = field
		}
	}
	return &snapshot
}

func (a *App) WatcherManager() *watcher.Manager     { return a.watcher }
func (a *App) TaskScheduler() *scheduler.Trigger    { return a.scheduler }
func (a *App) Metadata() *heyametadata.HeyaProvider { return a.heya }
func (a *App) DBPool() *pgxpool.Pool                { return a.db }
func (a *App) Diagnostics() *diagnostics.Collector  { return a.diagnostics }
func (a *App) RiverClient() *river.Client[pgx.Tx]   { return a.river }
func (a *App) Sessions() *sessions.Store            { return a.sessions }

// CoordinatorLeaseLost receives a non-nil error if the worker's dedicated
// PostgreSQL advisory-lock session ends unexpectedly. API, command, and finite
// queue-processing runtimes own no coordinator lease and return a nil channel.
func (a *App) CoordinatorLeaseLost() <-chan error {
	if a == nil || a.coordinatorLease == nil {
		return nil
	}
	monitor, ok := a.coordinatorLease.(interface{ Lost() <-chan error })
	if !ok {
		return nil
	}
	return monitor.Lost()
}

func (a *App) SetScheduler(s *scheduler.Trigger) { a.scheduler = s }

// StartScheduler binds the worker's cron loops to both the command context and
// App lifetime, and joins them during Close before their DB/River dependencies
// are released.
func (a *App) StartScheduler(ctx context.Context) {
	if a == nil || a.scheduler == nil {
		return
	}
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		a.scheduler.Run(workCtx)
	})
}

func (a *App) Tailscale() tailscale.Manager {
	a.networkMu.RLock()
	defer a.networkMu.RUnlock()
	return a.tailscale
}
func (a *App) SetTailscale(ts tailscale.Manager) {
	a.networkMu.Lock()
	a.tailscale = ts
	a.networkMu.Unlock()
}
func (a *App) Ingress() *ingress.Manager {
	a.networkMu.RLock()
	defer a.networkMu.RUnlock()
	return a.ingress
}
func (a *App) SetIngress(manager *ingress.Manager) {
	a.networkMu.Lock()
	a.ingress = manager
	a.networkMu.Unlock()
}

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

// startBackground admits App-owned fire-and-forget work while shutdown has not
// begun. Admission and Wait are serialized by backgroundMu, avoiding the
// WaitGroup Add-vs-Wait race that ad-hoc goroutines otherwise create.
func (a *App) startBackground(work func()) bool {
	if a == nil || work == nil {
		return false
	}
	a.backgroundMu.Lock()
	if a.backgroundClosed {
		a.backgroundMu.Unlock()
		return false
	}
	a.backgroundWG.Add(1)
	a.backgroundMu.Unlock()
	go func() {
		defer a.backgroundWG.Done()
		work()
	}()
	return true
}

// backgroundContext is cancelled when either the caller's runtime context or
// the App lifetime ends. Long-lived startup loops need both: the command
// context stops them promptly on a signal, while the App context makes Close
// self-contained when a caller forgets to cancel its parent first.
func (a *App) backgroundContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	lifetime := a.LifetimeContext()
	if lifetime == nil {
		return ctx, cancel
	}
	stopLifetime := context.AfterFunc(lifetime, cancel)
	return ctx, func() {
		stopLifetime()
		cancel()
	}
}

func (a *App) beginBackgroundShutdown() {
	a.backgroundMu.Lock()
	if !a.backgroundClosed {
		a.backgroundClosed = true
		if a.lifetimeCancel != nil {
			a.lifetimeCancel()
		}
	}
	a.backgroundMu.Unlock()
}

func (a *App) stopBackground() {
	a.beginBackgroundShutdown()
	a.backgroundWG.Wait()
}

// QuiesceBackground closes admission, cancels, and joins App-owned background
// work without closing the database or other request-serving resources. It is
// used by the serve shutdown coordinator before external network managers are
// closed; Close calls the same operation idempotently for every other runtime.
func (a *App) QuiesceBackground() {
	if a != nil {
		a.stopBackground()
	}
}

// UpdateTailscaleConfig overlays the in-memory tailscale snapshot for callers
// that manage persistence separately. SaveTailscaleSettings uses the same
// locked primitive while serializing persistence with the overlay. Env-sourced
// fields retain their provenance.
func (a *App) UpdateTailscaleConfig(enabled, https, funnel bool, hostname string) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.updateTailscaleConfigLocked(enabled, https, funnel, hostname)
}

func (a *App) updateTailscaleConfigLocked(enabled, https, funnel bool, hostname string) {
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

type appRuntimeMode uint8

const (
	appRuntimeAPI appRuntimeMode = iota
	appRuntimeWorker
	appRuntimeQueueProcessor
	appRuntimeCommand
)

func (m appRuntimeMode) executesWorkers() bool {
	return m == appRuntimeWorker || m == appRuntimeQueueProcessor
}

func (m appRuntimeMode) ownsCoordinatorLease() bool {
	return m == appRuntimeWorker
}

// New constructs the API runtime. Its River client can insert and manage jobs
// but owns no queues, worker goroutines, or River maintenance services. Queue
// execution belongs to NewWorker and NewQueueProcessor.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	return newApp(ctx, cfg, appRuntimeAPI)
}

// NewWorker constructs the dedicated background runtime with the complete
// River worker registry, queue migrations, filesystem watchers, and worker
// events relayed to the API process through Postgres. It acquires the database
// coordinator lease before performing any worker-owned setup.
func NewWorker(ctx context.Context, cfg *config.Config) (*App, error) {
	return newApp(ctx, cfg, appRuntimeWorker)
}

// NewQueueProcessor constructs the finite, manually-invoked queue-draining
// runtime. It can execute the same River jobs as NewWorker but intentionally
// does not claim the long-lived coordinator role.
func NewQueueProcessor(ctx context.Context, cfg *config.Config) (*App, error) {
	return newApp(ctx, cfg, appRuntimeQueueProcessor)
}

// NewCommand constructs the one-shot command runtime. It preserves the
// service/config behavior CLI commands historically received from New, but it
// does not own live HLS or audio-transcode session managers. A command should
// never disturb playback in a concurrently running API process merely by
// opening the service layer.
func NewCommand(ctx context.Context, cfg *config.Config) (*App, error) {
	return newApp(ctx, cfg, appRuntimeCommand)
}

func newApp(ctx context.Context, cfg *config.Config, runtimeMode appRuntimeMode) (*App, error) {
	// Passive mode is a read-mostly guest on someone else's DB (typically a
	// dev box pointed at production). Skip AutoMigrate so we never mutate the
	// target's schema to match this binary's branch — if the schemas differ,
	// failing local queries are a far safer outcome than altering prod.
	//
	// Worker migrations deliberately wait until after the coordinator lease is
	// held. Otherwise a second worker that will lose the lease can still mutate
	// the schema before discovering that it is not the coordinator. API and
	// command runtimes keep their historical migrate-before-connect ordering.
	if !cfg.PassiveMode.Value && !runtimeMode.ownsCoordinatorLease() {
		if err := AutoMigrate(cfg.DatabaseURL.Value); err != nil {
			return nil, err
		}
	} else if cfg.PassiveMode.Value {
		log.Warn().Msg("passive mode: skipping auto-migrate; this binary will NOT alter the target schema")
	}

	diagnosticCollector := diagnostics.NewCollector()
	dbOptions := databaseOptionsForRuntime(cfg, runtimeMode)
	dbOptions.QueryTracer = diagnosticCollector
	db, err := database.ConnectWithOptions(ctx, cfg.DatabaseURL.Value, dbOptions)
	if err != nil {
		return nil, err
	}
	if !cfg.PassiveMode.Value && runtimeMode == appRuntimeAPI {
		extensionCtx, cancel := context.WithTimeout(diagnostics.WithoutQueryTrace(ctx), 10*time.Second)
		if extensionErr := database.EnsurePGStatStatements(extensionCtx, db); extensionErr != nil {
			log.Warn().Err(extensionErr).Msg("pg_stat_statements could not be enabled automatically; query diagnostics will use the process tracer")
		}
		cancel()
	}

	// Establish ownership as soon as the database is live. Components created
	// below may start goroutines or own native/process resources even before an
	// App is assembled; a later constructor error must cancel and release all
	// of them, not merely close the database pool.
	lifetimeCtx, lifetimeCancel := newAppLifetime(ctx)
	var (
		tc             *transcoder.SessionManager
		tcCache        *transcoder.CacheManager
		audioSessions  *transcoder.AudioSessionManager
		textSearcher   *sonicanalysis.TextSearcher
		analyzer       *sonicanalysis.Analyzer
		sonicHolder    *sonicanalysis.Holder
		coordinator    *runtimelease.Lease
		relayPublisher *eventhub.RelayPublisher
		sessionLookup  *auth.AsyncSessionLookup
	)
	startupComplete := false
	defer func() {
		if startupComplete {
			return
		}
		lifetimeCancel()
		if audioSessions != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = audioSessions.Close(closeCtx)
			cancel()
		}
		if tc != nil {
			tc.Close()
		}
		if textSearcher != nil {
			textSearcher.Close()
		}
		if analyzer != nil {
			analyzer.Unload()
		}
		if sonicHolder != nil {
			sonicHolder.Close()
		}
		if relayPublisher != nil {
			relayPublisher.Close()
		}
		if sessionLookup != nil {
			sessionLookup.Close()
		}
		if coordinator != nil {
			if err := coordinator.Close(); err != nil {
				log.Warn().Err(err).Msg("coordinator lease release failed during startup cleanup")
			}
		}
		db.Close()
	}()

	// Take the singleton role before migrations, provider readiness checks,
	// relay goroutines, River setup, watcher construction, or any other
	// worker-owned side effect. Finite queue processing and one-shot commands
	// intentionally never enter this coordinator mode and remain lease-free.
	if runtimeMode.ownsCoordinatorLease() {
		coordinator, err = runtimelease.AcquireCoordinator(ctx, db)
		if err != nil {
			return nil, err
		}
		log.Info().Msg("worker coordinator lease acquired")
		if !cfg.PassiveMode.Value {
			if err := AutoMigrate(cfg.DatabaseURL.Value); err != nil {
				return nil, err
			}
		}
	}

	// Apply DB-backed media settings before constructing the cache and lazy
	// hardware provider that consume them. Env-owned fields remain locked.
	loadTranscoderConfigFromDB(ctx, db, cfg)
	analysis := mediaanalysis.New(lifetimeCtx, db)
	sessionLookup = auth.NewAsyncSessionLookup(lifetimeCtx, sqlc.New(db))

	dl := images.NewDownloader(cfg.DataDir.Value, images.TrustedSource{
		BaseURL: cfg.HeyaMetadataURL.Value, BearerToken: cfg.HeyaMetadataAPIKey.Value,
		ImageVariantWidth: heyametadata.ImageVariantWidth,
	})

	hm, err := heyametadata.NewClient(cfg.HeyaMetadataURL.Value, cfg.HeyaMetadataAPIKey.Value)
	if err != nil {
		return nil, err
	}
	// Long-lived API and worker runtimes depend on canonical metadata being
	// available at startup. One-shot commands should only fail if the specific
	// operation they run actually needs that provider; user/list/doctor-style
	// commands must not be held hostage by an unrelated remote health check.
	if !cfg.PassiveMode.Value && runtimeMode != appRuntimeCommand {
		readyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		readyErr := hm.Ready(readyCtx)
		cancel()
		if readyErr != nil {
			return nil, fmt.Errorf("HeyaMetadata is not ready: %w", readyErr)
		}
	}
	heya := heyametadata.NewHeyaProvider(hm, db).WithProviderCredentials(heyametadata.ProviderCredentials{
		LastFMAPIKey: cfg.LastfmAPIKey.Value,
	})
	acoustID, err := acoustid.New(acoustid.Options{
		BaseURL: cfg.AcoustIDBaseURL.Value, APIKey: cfg.AcoustIDAPIKey.Value,
		RequestsPerSecond: cfg.AcoustIDRequestsPerSecond.Value,
	})
	if err != nil {
		return nil, err
	}
	segmentService := communitysegments.New(db, communitysegments.Options{TheIntroDBAPIKey: cfg.TheIntroDBAPIKey.Value})

	log.Info().Str("url", cfg.HeyaMetadataURL.Value).Msg("canonical metadata provider registered via HeyaMetadata V2")

	hub := eventhub.New()

	LoadJobWorkersFromDB(ctx, db, cfg)
	var workerPublisher worker.EventPublisher = hub
	if runtimeMode.executesWorkers() {
		relayPublisher = eventhub.NewRelayPublisher(lifetimeCtx, db)
		workerPublisher = relayPublisher
	}

	m := matcher.New(db, matcher.DefaultOptions(), heya, mediaprobe.Probe)

	if runtimeMode == appRuntimeAPI && transcoder.IsFFmpegAvailable() {
		tcCache = transcoder.NewCacheManager(cfg.TranscodeCacheDir.Value, cfg.TranscodeCacheMaxGB.Value)
		// Provider resolves on first transcode session, not at startup —
		// keeps service.New fork-free. See hwaccel_provider.go for the
		// Network.framework/atfork rationale.
		hwAccelProvider := transcoder.NewHwAccelProvider(cfg.DataDir.Value, cfg.HWAccel.Value)
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
	// The worker-facing Holder must boot from the same durable/env-overlayed
	// settings the API reports. Defaults here made a restarted worker silently
	// ignore a persisted accelerator choice.
	bootSettings := effectiveSonicAnalysisSettingsFromDB(ctx, db)
	saCfg := sonicanalysis.Config{
		ModelsDir:   modelsDir,
		Accelerator: sonicanalysis.Accelerator(bootSettings.Accelerator),
	}
	sonicHolder = sonicanalysis.NewHolder(saCfg, 5*time.Minute)

	// Watcher hook + sonic enabled gate are both built in two passes:
	// worker.Setup wires the kickoff workers with indirection wrappers,
	// then after the App is fully constructed we assign the concrete
	// targets. We can't do that directly because workers can't be
	// re-wired after AddWorker — see lazyWatcher / lazyEnabled below.
	var watcherPauser worker.WatcherPauser        // assigned after watcher.NewManager
	var sonicEnabledFn func(context.Context) bool // assigned after App construction
	var embedBackfillFn worker.EmbedBackfillFn    // assigned after App construction
	var lastfmCredsFn worker.LastfmCredsFn        // assigned after App construction

	// Progress events must go through workerPublisher, not the raw local
	// hub: in the dedicated-worker process the local hub has no WS
	// subscribers, so per-item progress ("analyzing track X") would never
	// reach the API process. workerPublisher relays via pg_notify there
	// and degrades to the in-process hub in API/CLI mode.
	progress := worker.NewTaskProgressBroadcaster(workerPublisher)

	var riverClient *river.Client[pgx.Tx]
	if runtimeMode.executesWorkers() {
		riverClient, err = worker.Setup(ctx, worker.Config{
			DB:            db,
			DataDir:       cfg.DataDir.Value,
			HeyaMetadata:  hm,
			Heya:          heya,
			AcoustID:      acoustID,
			Segments:      segmentService,
			Matcher:       m,
			Downloader:    dl,
			MediaAnalysis: analysis,
			Hub:           workerPublisher,
			SonicHolder:   sonicHolder,
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
			LastfmCreds: func(ctx context.Context) (string, string) {
				if lastfmCredsFn == nil {
					return "", ""
				}
				return lastfmCredsFn(ctx)
			},
			Watcher:      lazyWatcher{ptr: &watcherPauser},
			Progress:     progress,
			Passive:      cfg.PassiveMode.Value,
			WorkerCounts: cfg.JobWorkerCounts(),
		})
	} else {
		riverClient, err = worker.NewInsertClient(db)
	}
	if err != nil {
		return nil, err
	}

	var wm *watcher.Manager
	if runtimeMode.executesWorkers() {
		wm = watcher.NewManager(db, riverClient, func(libraryID int64, force bool) {
			_ = worker.EnqueueKickoffLibraryScan(lifetimeCtx, riverClient, db, worker.KickoffLibraryScanArgs{
				LibraryID: libraryID,
				Force:     force,
			})
		})
		watcherPauser = wm
	}

	// Reverse-wire SonicEnabled now that we'll have an App with the
	// system_settings accessor. Assignment happens before any worker
	// fires (StartWorkers is called later from cmd/worker), so the
	// kickoff sees a fully-initialised closure on first invocation.
	_ = sonicEnabledFn // keep variable address stable while we capture it; assigned below

	// Auxiliary sonic-analysis surfaces — text-only search + the model
	// fetcher used by the Settings page. The Holder + Analyzer above
	// own the per-track analysis path. saCfg + modelsDir were
	// initialised in the worker.Setup block; reuse them here.
	textSearcher = sonicanalysis.NewTextSearcher(saCfg)
	modelFetcher := sonicanalysis.NewModelFetcher(modelsDir, "")
	analyzer = sonicanalysis.NewAnalyzer(saCfg)

	// Optional embedding recommendation engine — its own model dir + fetcher
	// (multilingual BGE-M3), lazy embedder loaded on first use when enabled.
	recModelsDir := cfg.DataDir.Value + "/models/recommendations"
	recFetcher := sonicanalysis.NewModelFetcherWithManifest(recModelsDir, "", recommendationsMLManifest())

	resizer, err := imageserve.New(cfg.DataDir.Value + "/images/resized")
	if err != nil {
		return nil, fmt.Errorf("init image resizer: %w", err)
	}

	// Managed llama-server runtime for the AI subsystem (mode=local). Spawns
	// nothing until first use; artifacts live under DataDir/llm.
	llmLocal := llm.NewLocalRuntime(cfg.DataDir.Value + "/llm")
	llmLocal.Bind(lifetimeCtx)
	imageRuntime := imagegen.NewRuntime(cfg.DataDir.Value + "/imagegen")

	// Server-side cast manager. Constructed always (accessors stay
	// nil-safe); discovery only starts via StartCast when cast.enabled.
	castMgr := cast.New(cfg.DataDir.Value)
	castMgr.SetHub(hub)

	app := &App{
		config:           cfg,
		db:               db,
		diagnostics:      diagnosticCollector,
		sessionLookup:    sessionLookup,
		securityEvents:   securityevents.New(200),
		coordinatorLease: coordinator,
		matcher:          m,
		downloader:       dl,
		river:            riverClient,
		watcher:          wm,
		heya:             heya,
		transcoder:       tc,
		transcodeCache:   tcCache,
		audioSessions:    audioSessions,
		hub:              hub,
		relayPublisher:   relayPublisher,
		textSearcher:     textSearcher,
		modelFetcher:     modelFetcher,
		analyzer:         analyzer,
		sonicHolder:      sonicHolder,
		taskProgress:     progress,
		recFetcher:       recFetcher,
		recModelsDir:     recModelsDir,
		imageResizer:     resizer,
		mediaAnalysis:    analysis,
		radioBrowser:     radiobrowser.New(),
		podcastIndex:     podcastindex.New(cfg.PodcastIndexKey.Value, cfg.PodcastIndexSecret.Value),
		sessions:         sessions.New(lifetimeCtx, hub),
		llmLocal:         llmLocal,
		imageRuntime:     imageRuntime,
		castMgr:          castMgr,
		playbackGrants:   playbackgrant.New(),
		castAllowedUsers: make(map[int64]struct{}),
		lifetimeCtx:      lifetimeCtx,
		lifetimeCancel:   lifetimeCancel,
		startedAt:        time.Now(),
	}
	// Trusted-network policy is needed by both the authentication handlers and
	// the ingress config assembled immediately after New returns. Load its DB
	// overlay before either begins serving requests; env provenance remains
	// authoritative and locks runtime edits.
	app.LoadTrustedNetworksFromDB(ctx)

	// Wire the SonicEnabled closure now that the App can answer the
	// system_settings query. Worker.Setup captured a closure pointing
	// at sonicEnabledFn; assigning here makes that closure live before
	// any kickoff fires.
	sonicEnabledFn = app.SonicAnalysisEnabled
	embedBackfillFn = app.backfillEmbeddingsForTask
	lastfmCredsFn = app.lastfmCredentials

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
	if !cfg.PassiveMode.Value && !runtimeMode.executesWorkers() {
		app.LoadTailscaleFromDB(ctx)
		// Same reasoning as tailscale: a borrowed prod DB's remote.enabled
		// must not map ports / issue certs from a dev checkout.
		app.LoadRemoteFromDB(ctx)
	}
	// Env bootstrap. Order: admin first (libraries.created_by FK requires
	// at least one user). Failures here are logged and continue — a misformed
	// env var shouldn't kill the server. The /api/config/sources response
	// will reflect whatever actually got persisted. Skipped in passive mode:
	// a dev box's local HEYA_ADMIN_* / HEYA_LIBRARY_* must never overwrite the
	// users and library paths of the production DB it's borrowing.
	if !cfg.PassiveMode.Value && !runtimeMode.executesWorkers() {
		if err := app.BootstrapAdminFromEnv(ctx); err != nil {
			log.Warn().Err(err).Msg("HEYA_ADMIN_* bootstrap failed")
		}
		if err := app.BootstrapLibrariesFromEnv(ctx); err != nil {
			log.Warn().Err(err).Msg("HEYA_LIBRARY_* bootstrap failed")
		}
	}
	app.ReportUnsupportedLibraryPaths(ctx)

	startupComplete = true
	return app, nil
}

func newAppLifetime(parent context.Context) (context.Context, context.CancelFunc) {
	// Startup still uses parent directly, but successfully constructed App-owned
	// goroutines and subprocesses live until App.Close. Preserve request values
	// while stripping the command's signal cancellation/deadline so shutdown can
	// give workers their explicit graceful-stop window first.
	return context.WithCancel(context.WithoutCancel(parent))
}

func databaseOptionsForRuntime(cfg *config.Config, _ appRuntimeMode) database.Options {
	return database.Options{
		MaxConns: int32(cfg.DatabaseMaxConns.Value),
		MinConns: int32(cfg.DatabaseMinConns.Value),
	}
}

// StartPlaylistSync starts the periodic external-playlist reconciliation loop.
// It belongs to the dedicated worker runtime so multiple API replicas cannot
// perform the same sync concurrently.
func (a *App) StartPlaylistSync(ctx context.Context) {
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		a.runPlaylistSyncLoop(workCtx)
	})
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
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		if err := a.modelFetcher.Run(workCtx); err != nil && workCtx.Err() == nil {
			log.Err(err).Msg("sonicanalysis: model fetcher exited with error")
		}
	})
}

// TriggerSonicAnalysisFetch starts an App-owned model verification/download
// detached from the HTTP request that requested it. The work is cancelled and
// joined by Close, so it cannot keep writing model files after the rest of the
// application has been torn down.
func (a *App) TriggerSonicAnalysisFetch() bool {
	if a == nil || a.modelFetcher == nil {
		return false
	}
	return a.startBackground(func() {
		if err := a.modelFetcher.Run(a.LifetimeContext()); err != nil && a.LifetimeContext().Err() == nil {
			log.Warn().Err(err).Msg("sonicanalysis: requested model fetch failed")
		}
	})
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

func (l lazyWatcher) SuppressGeneratedWrite(output generatedwrite.Output) error {
	if l.ptr == nil || *l.ptr == nil {
		return errors.New("generated sidecar acknowledger is not initialized")
	}
	suppressor, ok := (*l.ptr).(worker.GeneratedWriteSuppressor)
	if !ok {
		return errors.New("configured watcher cannot acknowledge generated sidecars")
	}
	return suppressor.SuppressGeneratedWrite(output)
}

func (a *App) StartWorkers(ctx context.Context) error {
	if a == nil || a.river == nil {
		return errors.New("worker client is not available")
	}
	a.riverMu.Lock()
	defer a.riverMu.Unlock()
	if a.closing {
		return errAppClosing
	}
	if a.riverStarted {
		return nil
	}
	if err := a.river.Start(ctx); err != nil {
		return err
	}
	a.riverStarted = true
	return nil
}

func (a *App) QueueCounts(ctx context.Context) (pending, running int) {
	if counts, err := queueops.CountActive(ctx, a.db); err == nil {
		pending = counts.Pending
		running = counts.Running
	}
	return
}

func (a *App) StartWatchers(ctx context.Context) error {
	if a.watcher == nil {
		return errors.New("filesystem watchers are only available in the worker runtime")
	}
	if err := a.watcher.StartAll(ctx); err != nil {
		return err
	}
	return nil
}

// StartWatchersBackground starts watcher discovery without delaying River or
// scheduler startup. The work remains App-owned: Close cancels it through the
// worker context and waits for StartAll to return before releasing the DB.
func (a *App) StartWatchersBackground(ctx context.Context) {
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		if err := a.StartWatchers(workCtx); err != nil && workCtx.Err() == nil {
			log.Warn().Err(err).Msg("failed to start filesystem watchers")
		}
	})
}

func (a *App) StopWorkers(ctx context.Context) error {
	if a == nil || a.river == nil {
		return nil
	}
	a.riverMu.Lock()
	defer a.riverMu.Unlock()
	if !a.riverStarted {
		return nil
	}
	// Graceful first: try to let in-flight jobs finish. If the context
	// times out, escalate to StopAndCancel which interrupts running
	// jobs so we don't leak River goroutines holding pool connections
	// (the cause of pgxpool.Close hangs we've seen under air reloads).
	if err := a.river.Stop(ctx); err == nil {
		a.riverStarted = false
		return nil
	} else if ctx.Err() == nil {
		return err
	}
	hardCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := a.river.StopAndCancel(hardCtx)
	if err == nil {
		a.riverStarted = false
	}
	return err
}

// closeWorkers makes App.Close the final owner of River even when a command
// returns early after StartWorkers. On a bounded stop timeout it waits for the
// already-hard-cancelled client before dependent models, the coordinator lease,
// or the database can be torn down underneath a lingering job.
func (a *App) closeWorkers() {
	a.riverMu.Lock()
	a.closing = true
	started := a.riverStarted
	a.riverMu.Unlock()
	if !started {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	err := a.StopWorkers(ctx)
	cancel()
	if err == nil {
		return
	}
	log.Warn().Err(err).Msg("River shutdown exceeded its bounded stop; waiting before releasing worker resources")

	// StopWorkers only leaves riverStarted true after both graceful and hard
	// stop contexts expired. River's Stopped channel was established by the
	// successful Start and closes when its goroutines actually finish.
	a.riverMu.Lock()
	if !a.riverStarted {
		a.riverMu.Unlock()
		return
	}
	stopped := a.river.Stopped()
	a.riverMu.Unlock()
	if stopped != nil {
		<-stopped
	}
	a.riverMu.Lock()
	a.riverStarted = false
	a.riverMu.Unlock()
}

func (a *App) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		// Cast sessions first, before lifetimeCancel SIGTERMs the child
		// processes out from under them — receivers should see the graceful
		// ACTION=STOP → TEARDOWN path, not a torn connection.
		if a.castMgr != nil {
			a.castMgr.Stop()
		}
		// Cancel App-owned reconciliation/arming before joining the watcher.
		// Reconcile may be blocked in a DB query while holding its serialization
		// lock; cancellation lets StopAll acquire that lock and drain safely.
		a.beginBackgroundShutdown()
		// Stop filesystem producers before draining River, then make River's
		// terminal state part of App ownership rather than relying on each
		// command's happy-path shutdown sequence.
		if a.watcher != nil {
			a.watcher.StopAll()
		}
		// Background admission is already closed above. Its join can overlap
		// River's graceful stop, and none can add fresh queue/DB work now.
		a.closeWorkers()
		// With queue work joined, finish joining request-detached orchestration
		// before closing the reusable analysis service or its database.
		a.stopBackground()
		if a.mediaAnalysis != nil {
			a.mediaAnalysis.Close()
		}
		if a.relayPublisher != nil {
			a.relayPublisher.Close()
		}
		if a.sessionLookup != nil {
			a.sessionLookup.Close()
		}
		if a.sessions != nil {
			a.sessions.Close()
		}
		if a.hub != nil {
			a.hub.Close()
		}
		if a.llmLocal != nil {
			// lifetimeCancel above already signalled the subprocess; Stop waits
			// for it to actually exit so we never leak a llama-server.
			a.llmLocal.Stop()
		}
		if a.audioSessions != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := a.audioSessions.Close(closeCtx); err != nil {
				log.Warn().Err(err).Msg("audio transcode shutdown did not finish")
			}
			cancel()
		}
		if a.transcoder != nil {
			a.transcoder.Close()
		}
		if a.textSearcher != nil {
			a.textSearcher.Close()
		}
		if a.analyzer != nil {
			a.analyzer.Unload()
		}
		if a.sonicHolder != nil {
			a.sonicHolder.Close()
		}
		a.resetRecEmbedder()
		// Keep the singleton role until every App-owned worker resource above
		// has stopped, then release it while the pool is still usable. The worker
		// command stops River before its deferred App.Close reaches this point.
		if a.coordinatorLease != nil {
			if err := a.coordinatorLease.Close(); err != nil {
				log.Warn().Err(err).Msg("coordinator lease release failed")
			}
		}
		if a.db != nil {
			a.db.Close()
		}
	})
}
