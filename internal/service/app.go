package service

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/config"
	"github.com/karbowiak/kura/internal/database"
	"github.com/karbowiak/kura/internal/images"
	"github.com/karbowiak/kura/internal/matcher"
	"github.com/karbowiak/kura/internal/metadata"
	"github.com/karbowiak/kura/internal/metadata/musicbrainz"
	"github.com/karbowiak/kura/internal/metadata/openlibrary"
	"github.com/karbowiak/kura/internal/metadata/tmdb"
	"github.com/karbowiak/kura/internal/scanner"
	"github.com/karbowiak/kura/internal/watcher"
	"github.com/karbowiak/kura/internal/worker"
	"github.com/riverqueue/river"
)

type App struct {
	Config     *config.Config
	DB         *pgxpool.Pool
	Scanner    *scanner.Scanner
	Matcher    *matcher.Matcher
	Downloader *images.Downloader
	River      *river.Client[pgx.Tx]
	Watcher    *watcher.Manager
	Providers  []metadata.Provider
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	dl := images.NewDownloader(cfg.DataDir)
	sc := scanner.New(db)

	var providers []metadata.Provider
	if cfg.TMDBToken != "" {
		providers = append(providers, tmdb.NewProvider(cfg.TMDBToken))
	}
	providers = append(providers, musicbrainz.NewProvider())
	providers = append(providers, openlibrary.NewProvider())

	m := matcher.New(db, dl, matcher.DefaultOptions(), providers...)

	riverClient, err := worker.Setup(ctx, worker.Config{
		DB:         db,
		Matcher:    m,
		Downloader: dl,
		Providers:  providers,
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
		Providers:  providers,
	}, nil
}

func (a *App) StartWorkers(ctx context.Context) error {
	return a.River.Start(ctx)
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
