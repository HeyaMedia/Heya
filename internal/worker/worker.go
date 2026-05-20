package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/rs/zerolog/log"
)

var _ pgx.Tx // ensure import used

type Config struct {
	DB               *pgxpool.Pool
	DataDir          string
	TMDBToken        string
	Matcher          *matcher.Matcher
	Downloader       *images.Downloader
	Providers        []metadata.Provider
	ArtworkProviders []metadata.ArtworkProvider
	TranscodeCache   *transcoder.CacheManager
}

func Setup(ctx context.Context, cfg Config) (*river.Client[pgx.Tx], error) {
	migrator, err := rivermigrate.New(riverpgxv5.New(cfg.DB), nil)
	if err != nil {
		return nil, fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("river migrate: %w", err)
	}
	log.Info().Msg("river migrations applied")

	workers := river.NewWorkers()
	river.AddWorker(workers, &ScanLibraryWorker{DB: cfg.DB})
	river.AddWorker(workers, &ProcessFileWorker{DB: cfg.DB})
	river.AddWorker(workers, &MetadataMatchWorker{DB: cfg.DB, Matcher: cfg.Matcher, Providers: cfg.Providers})
	river.AddWorker(workers, &MetadataFetchWorker{DB: cfg.DB, Matcher: cfg.Matcher, Providers: cfg.Providers})
	river.AddWorker(workers, &DownloadImageWorker{DB: cfg.DB, Downloader: cfg.Downloader})
	river.AddWorker(workers, &FFProbeWorker{DB: cfg.DB})
	river.AddWorker(workers, &DetectLocalAssetsWorker{DB: cfg.DB, DataDir: cfg.DataDir})
	river.AddWorker(workers, &PersonFetchWorker{DB: cfg.DB, TMDBToken: cfg.TMDBToken})
	river.AddWorker(workers, &EnrichmentWorker{DB: cfg.DB, ArtworkProviders: cfg.ArtworkProviders})
	river.AddWorker(workers, &TranscodeWorker{DB: cfg.DB, Cache: cfg.TranscodeCache})
	river.AddWorker(workers, &SoftDeleteWorker{DB: cfg.DB})

	client, err := river.NewClient(riverpgxv5.New(cfg.DB), &river.Config{
		Queues: map[string]river.QueueConfig{
			"scan":      {MaxWorkers: 2},
			"process":   {MaxWorkers: 4},
			"metadata":  {MaxWorkers: 2},
			"images":    {MaxWorkers: 4},
			"transcode": {MaxWorkers: 1},
			river.QueueDefault: {MaxWorkers: 2},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}

	return client, nil
}
