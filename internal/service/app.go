package service

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/watcher"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type App struct {
	Config         *config.Config
	DB             *pgxpool.Pool
	Matcher        *matcher.Matcher
	Downloader     *images.Downloader
	River          *river.Client[pgx.Tx]
	Watcher        *watcher.Manager
	Registry       *metadata.Registry
	Transcoder     *transcoder.SessionManager
	TranscodeCache *transcoder.CacheManager
	Hub            *eventhub.Hub
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

	registry := metadata.NewRegistry()

	tmdbProvider := heyamedia.NewTMDBProvider(hm)
	registry.Register(tmdbProvider)
	registry.RegisterArtwork(heyamedia.NewTMDBArtworkProvider(hm))

	registry.Register(heyamedia.NewTVDBProvider(hm))

	omdbProvider := heyamedia.NewOMDBProvider(hm)
	registry.Register(omdbProvider)
	registry.RegisterRatings(omdbProvider)

	registry.RegisterArtwork(heyamedia.NewFanartProvider(hm))

	registry.Register(heyamedia.NewMusicBrainzProvider(hm))
	registry.Register(heyamedia.NewOpenLibraryProvider(hm))

	log.Info().Str("url", cfg.HeyaMediaURL).Msg("metadata providers registered via heya.media")

	hub := eventhub.New()

	m := matcher.New(db, dl, matcher.DefaultOptions(), registry)

	var tc *transcoder.SessionManager
	var tcCache *transcoder.CacheManager
	var hwAccel transcoder.HwAccelConfig
	if transcoder.IsFFmpegAvailable() {
		tcCache = transcoder.NewCacheManager(cfg.TranscodeCacheDir, cfg.TranscodeCacheMaxGB)

		switch cfg.HWAccel {
		case "auto":
			accelType := transcoder.DetectHardwareAccel()
			hwAccel = transcoder.BuildHwAccelConfig(accelType)
			log.Info().Str("hwaccel", string(accelType)).Msg("hardware acceleration detected")
		case "none", "":
			hwAccel = transcoder.BuildHwAccelConfig(transcoder.HwAccelNone)
		default:
			hwAccel = transcoder.BuildHwAccelConfig(transcoder.HwAccelType(cfg.HWAccel))
			log.Info().Str("hwaccel", cfg.HWAccel).Msg("hardware acceleration forced")
		}

		tc = transcoder.NewSessionManager(tcCache, hwAccel)
	}

	riverClient, err := worker.Setup(ctx, worker.Config{
		DB:             db,
		DataDir:        cfg.DataDir,
		HeyaMedia:      hm,
		Matcher:        m,
		Downloader:     dl,
		Registry:       registry,
		TranscodeCache: tcCache,
		HWAccel:        hwAccel,
		Hub:            hub,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	wm := watcher.NewManager(db, riverClient)

	return &App{
		Config:         cfg,
		DB:             db,
		Matcher:        m,
		Downloader:     dl,
		River:          riverClient,
		Watcher:        wm,
		Registry:       registry,
		Transcoder:     tc,
		TranscodeCache: tcCache,
		Hub:            hub,
	}, nil
}

func (a *App) StartWorkers(ctx context.Context) error {
	return a.River.Start(ctx)
}

func (a *App) QueueCounts(ctx context.Context) (pending, running int) {
	row := a.DB.QueryRow(ctx, "SELECT count(*) FILTER (WHERE state = 'available' OR state = 'retryable'), count(*) FILTER (WHERE state = 'running') FROM river_job")
	row.Scan(&pending, &running)
	return
}

func (a *App) EnqueuePendingFiles(ctx context.Context, libraryID int64) (int, error) {
	q := sqlc.New(a.DB)
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
		a.River.Insert(ctx, worker.ProcessFileArgs{
			LibraryFileID: f.ID,
			LibraryID:     libraryID,
			FilePath:      f.Path,
		}, nil)
	}

	return len(files), nil
}

func (a *App) StartWatchers(ctx context.Context) error {
	if err := a.Watcher.StartAll(ctx); err != nil {
		return err
	}
	watcher.SetupPeriodicScans(ctx, a.DB, a.River)
	return nil
}

func (a *App) StopWorkers(ctx context.Context) error {
	if a.River != nil {
		return a.River.Stop(ctx)
	}
	return nil
}

func (a *App) Close() {
	if a.Watcher != nil {
		a.Watcher.StopAll()
	}
	if a.DB != nil {
		a.DB.Close()
	}
}

