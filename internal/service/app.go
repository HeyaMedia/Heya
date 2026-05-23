package service

import (
	"context"
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
	hub            *eventhub.Hub
	scheduler      *scheduler.Runner
	scanTask       *scheduler.ScanLibrariesTask
	tailscale      *tailscale.Server
}

// Accessor methods for handler packages that need App internals.

func (a *App) SessionLookup() auth.SessionLookup               { return sqlc.New(a.db) }
func (a *App) TranscoderSessions() *transcoder.SessionManager  { return a.transcoder }
func (a *App) TranscoderCache() *transcoder.CacheManager       { return a.transcodeCache }
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

// UpdateTailscaleConfig swaps the in-memory tailscale config for new values.
// Callers persist via config.SaveTailscale separately — this just updates
// the snapshot used by future reads.
func (a *App) UpdateTailscaleConfig(ts config.TailscaleConfig) {
	a.config.Tailscale = ts
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	if err := AutoMigrate(cfg.DatabaseURL); err != nil {
		return nil, err
	}

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	dl := images.NewDownloader(cfg.DataDir)

	hm := heyamedia.NewClient(cfg.HeyaMediaURL)
	heya := heyamedia.NewHeyaProvider(hm)

	log.Info().Str("url", cfg.HeyaMediaURL).Msg("metadata provider registered via heya.media")

	hub := eventhub.New()

	m := matcher.New(db, matcher.DefaultOptions(), heya)

	var tc *transcoder.SessionManager
	var tcCache *transcoder.CacheManager
	var hwAccelProvider *transcoder.HwAccelProvider
	if transcoder.IsFFmpegAvailable() {
		tcCache = transcoder.NewCacheManager(cfg.TranscodeCacheDir, cfg.TranscodeCacheMaxGB)
		// Provider resolves on first transcode session, not at startup —
		// keeps service.New fork-free. See hwaccel_provider.go for the
		// Network.framework/atfork rationale.
		hwAccelProvider = transcoder.NewHwAccelProvider(cfg.DataDir, cfg.HWAccel)
		tc = transcoder.NewSessionManager(tcCache, hwAccelProvider, transcoder.NewFFmpegBuilder())
	}

	riverClient, err := worker.Setup(ctx, worker.Config{
		DB:             db,
		DataDir:        cfg.DataDir,
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

	return &App{
		config:         cfg,
		db:             db,
		matcher:        m,
		downloader:     dl,
		river:          riverClient,
		watcher:        wm,
		heya:           heya,
		transcoder:     tc,
		transcodeCache: tcCache,
		hub:            hub,
		scanTask:       scanTask,
	}, nil
}

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
	if a.watcher != nil {
		a.watcher.StopAll()
	}
	if a.db != nil {
		a.db.Close()
	}
}
