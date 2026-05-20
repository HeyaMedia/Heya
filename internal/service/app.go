package service

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/fanart"
	"github.com/karbowiak/heya/internal/metadata/musicbrainz"
	"github.com/karbowiak/heya/internal/metadata/openlibrary"
	"github.com/karbowiak/heya/internal/metadata/tmdb"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/scanner"
	"github.com/karbowiak/heya/internal/watcher"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
)

type App struct {
	Config           *config.Config
	DB               *pgxpool.Pool
	Scanner          *scanner.Scanner
	Matcher          *matcher.Matcher
	Downloader       *images.Downloader
	River            *river.Client[pgx.Tx]
	Watcher          *watcher.Manager
	Providers        []metadata.Provider
	ArtworkProviders []metadata.ArtworkProvider
	Transcoder       *transcoder.SessionManager
	TranscodeCache   *transcoder.CacheManager
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	dl := images.NewDownloader(cfg.DataDir)
	sc := scanner.New(db)

	var providers []metadata.Provider
	var artworkProviders []metadata.ArtworkProvider

	if cfg.TMDBToken != "" {
		tmdbProvider := tmdb.NewProvider(cfg.TMDBToken)
		providers = append(providers, tmdbProvider)
		artworkProviders = append(artworkProviders, tmdbProvider)
	}
	providers = append(providers, musicbrainz.NewProvider())
	providers = append(providers, openlibrary.NewProvider())

	if cfg.FanartAPIKey != "" {
		artworkProviders = append(artworkProviders, fanart.NewProvider(cfg.FanartAPIKey))
	}

	m := matcher.New(db, dl, matcher.DefaultOptions(), providers...)

	var tc *transcoder.SessionManager
	var tcCache *transcoder.CacheManager
	if transcoder.IsFFmpegAvailable() {
		tcCache = transcoder.NewCacheManager(cfg.TranscodeCacheDir, cfg.TranscodeCacheMaxGB)
		tc = transcoder.NewSessionManager(tcCache)
	}

	riverClient, err := worker.Setup(ctx, worker.Config{
		DB:               db,
		DataDir:          cfg.DataDir,
		TMDBToken:        cfg.TMDBToken,
		Matcher:          m,
		Downloader:       dl,
		Providers:        providers,
		ArtworkProviders: artworkProviders,
		TranscodeCache:   tcCache,
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	wm := watcher.NewManager(db, riverClient)

	return &App{
		Config:     cfg,
		DB:         db,
		Scanner:    sc,
		Matcher:    m,
		Downloader: dl,
		River:      riverClient,
		Watcher:    wm,
		Providers:        providers,
		ArtworkProviders: artworkProviders,
		Transcoder:       tc,
		TranscodeCache:   tcCache,
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
