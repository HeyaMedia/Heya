package database

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Options struct {
	MaxConns int32
	MinConns int32
}

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return ConnectWithOptions(ctx, databaseURL, Options{MaxConns: 15, MinConns: 2})
}

func ConnectWithOptions(ctx context.Context, databaseURL string, opts Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if opts.MaxConns <= 0 {
		opts.MaxConns = 15
	}
	if opts.MinConns < 0 {
		opts.MinConns = 0
	}
	if opts.MinConns > opts.MaxConns {
		opts.MinConns = opts.MaxConns
	}

	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
