package service

import (
	"context"

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
)

type App struct {
	Config     *config.Config
	DB         *pgxpool.Pool
	Scanner    *scanner.Scanner
	Matcher    *matcher.Matcher
	Downloader *images.Downloader
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

	return &App{
		Config:     cfg,
		DB:         db,
		Scanner:    sc,
		Matcher:    m,
		Downloader: dl,
	}, nil
}

func (a *App) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}
