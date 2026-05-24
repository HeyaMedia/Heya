package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/rs/zerolog/log"
)

var _ pgx.Tx // ensure import used

// MatchService abstracts the matcher operations used by workers so that tests
// can supply lightweight fakes instead of a fully-wired *matcher.Matcher.
type MatchService interface {
	MatchSingleFile(ctx context.Context, file sqlc.LibraryFile, mediaType sqlc.MediaType, libraryID int64) (matcher.MatchInfo, error)
	StoreEntityMetadata(ctx context.Context, mediaItemID int64, kind metadata.MediaKind, detail *metadata.MediaDetail)
	StoreRichMetadata(ctx context.Context, mediaItemID int64, detail *metadata.MediaDetail)
	ResolveMatch(ctx context.Context, fileID, candidateID int64) error
	RefreshMusicArtist(ctx context.Context, artistID int64) (matcher.RefreshArtistResult, error)
	MediaItemIDForArtist(ctx context.Context, artistID int64) (int64, error)
}

// EventPublisher abstracts the event-emitting side of the event hub so that
// workers can be tested without a live Hub.
type EventPublisher interface {
	Emit(eventType eventhub.EventType, payload any)
}

type Config struct {
	DB             *pgxpool.Pool
	DataDir        string
	HeyaMedia      *heyamedia.Client
	Heya           *heyamedia.HeyaProvider
	Matcher        MatchService
	Downloader     *images.Downloader
	TranscodeCache *transcoder.CacheManager
	HWAccel        *transcoder.HwAccelProvider
	Hub            EventPublisher
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
	river.AddWorker(workers, &ProcessFileWorker{DB: cfg.DB})
	river.AddWorker(workers, &MetadataMatchWorker{DB: cfg.DB, Matcher: cfg.Matcher, Heya: cfg.Heya, Hub: cfg.Hub})
	river.AddWorker(workers, &EnrichMediaItemWorker{DB: cfg.DB, Matcher: cfg.Matcher, Heya: cfg.Heya, Hub: cfg.Hub})
	river.AddWorker(workers, &DownloadImageWorker{DB: cfg.DB, Downloader: cfg.Downloader, HeyaMedia: cfg.HeyaMedia, Hub: cfg.Hub})
	river.AddWorker(workers, &FFProbeWorker{DB: cfg.DB})
	river.AddWorker(workers, &DetectLocalAssetsWorker{DB: cfg.DB, DataDir: cfg.DataDir})
	river.AddWorker(workers, &PersonFetchWorker{DB: cfg.DB, HeyaMedia: cfg.HeyaMedia})
	river.AddWorker(workers, &EnrichmentWorker{DB: cfg.DB, Heya: cfg.Heya})
	river.AddWorker(workers, &RatingsFetchWorker{DB: cfg.DB, Heya: cfg.Heya})
	river.AddWorker(workers, &SaveNFOWorker{DB: cfg.DB})
	river.AddWorker(workers, &SaveImagesWorker{DB: cfg.DB})
	river.AddWorker(workers, &ForceRefreshMetadataWorker{DB: cfg.DB})
	river.AddWorker(workers, &ForceRefreshImagesWorker{DB: cfg.DB})
	river.AddWorker(workers, &TranscodeWorker{DB: cfg.DB, Cache: cfg.TranscodeCache, HWAccel: cfg.HWAccel})
	river.AddWorker(workers, &SoftDeleteWorker{DB: cfg.DB, Hub: cfg.Hub})
	river.AddWorker(workers, &SaveMusicNFOWorker{DB: cfg.DB})
	river.AddWorker(workers, &ScanTrackLoudnessWorker{DB: cfg.DB})
	river.AddWorker(workers, &ScanAlbumLoudnessWorker{DB: cfg.DB})

	client, err := river.NewClient(riverpgxv5.New(cfg.DB), &river.Config{
		Queues: map[string]river.QueueConfig{
			"process":          {MaxWorkers: 4},
			"metadata":         {MaxWorkers: 1}, // unified enrich queue — serialise heya.media calls so cold artist lookups don't pile up on the upstream. priority bands 1=watcher/view, 2=movies+tv, 3=music+books
			"images":           {MaxWorkers: 4},
			"ratings":          {MaxWorkers: 1},
			"saver":            {MaxWorkers: 2},
			"transcode":        {MaxWorkers: 1},
			"loudness":         {MaxWorkers: 1}, // ebur128 is CPU-bound (~10-20× real-time) — one at a time so it can't starve scans
			river.QueueDefault: {MaxWorkers: 2},
		},
		// Backstop for jobs that crashed mid-run (process died, OS
		// killed it, etc.) — River reassigns them after this window.
		// Set above the longest legitimate job runtime so River doesn't
		// preempt a still-working worker. Artist enrich can sit at
		// ~120s under HeyaMedia rate-limit backoff, and album loudness
		// can take a few minutes on long albums; 10 minutes covers both
		// with margin while still giving reasonable crash-recovery.
		RescueStuckJobsAfter: 10 * time.Minute,
		Workers:              workers,
	})
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}

	return client, nil
}
