package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/config"
	"github.com/karbowiak/kura/internal/database"
)

type App struct {
	Config *config.Config
	DB     *pgxpool.Pool
}

func New(ctx context.Context, cfg *config.Config) (*App, error) {
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	return &App{
		Config: cfg,
		DB:     db,
	}, nil
}

func (a *App) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}
