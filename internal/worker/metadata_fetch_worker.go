package worker

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type MetadataFetchWorker struct {
	river.WorkerDefaults[MetadataFetchArgs]
	DB        *pgxpool.Pool
	Matcher   *matcher.Matcher
	Providers []metadata.Provider
}

func (w *MetadataFetchWorker) Work(ctx context.Context, job *river.Job[MetadataFetchArgs]) error {
	var provider metadata.Provider
	for _, p := range w.Providers {
		if p.Name() == job.Args.ProviderName {
			provider = p
			break
		}
	}
	if provider == nil {
		log.Warn().Str("provider", job.Args.ProviderName).Msg("provider not found")
		return nil
	}

	detail, err := provider.GetDetail(ctx, job.Args.ProviderID)
	if err != nil {
		log.Warn().Err(err).Str("id", job.Args.ProviderID).Msg("metadata fetch failed")
		return nil
	}

	mediaType := sqlc.MediaType(job.Args.MediaType)
	kind := matcher.MediaTypeToKind(mediaType)

	w.Matcher.StoreEntityMetadata(ctx, job.Args.MediaItemID, kind, detail)
	w.Matcher.StoreRichMetadata(ctx, job.Args.MediaItemID, detail)

	client := river.ClientFromContext[pgx.Tx](ctx)

	client.Insert(ctx, DetectLocalAssetsArgs{
		MediaItemID:   job.Args.MediaItemID,
		LibraryFileID: job.Args.LibraryFileID,
		FilePath:      job.Args.FilePath,
		MediaType:     job.Args.MediaType,
	}, nil)

	if detail.PosterURL != "" {
		client.Insert(ctx, DownloadImageArgs{
			MediaItemID: job.Args.MediaItemID,
			EntityType:  "media",
			URL:         detail.PosterURL,
			AssetType:   "poster",
			MediaType:   job.Args.MediaType,
		}, nil)
	}
	if detail.BackdropURL != "" {
		client.Insert(ctx, DownloadImageArgs{
			MediaItemID: job.Args.MediaItemID,
			EntityType:  "media",
			URL:         detail.BackdropURL,
			AssetType:   "backdrop",
			MediaType:   job.Args.MediaType,
		}, nil)
	}

	client.Insert(ctx, EnrichmentArgs{
		MediaItemID: job.Args.MediaItemID,
		MediaType:   job.Args.MediaType,
	}, nil)

	w.enqueuePersonFetches(ctx, client, detail)

	log.Info().
		Int64("media_id", job.Args.MediaItemID).
		Int("cast", len(detail.Cast)).
		Int("crew", len(detail.Crew)).
		Int("keywords", len(detail.Keywords)).
		Int("videos", len(detail.Videos)).
		Msg("metadata fetch complete")

	return nil
}

func (w *MetadataFetchWorker) enqueuePersonFetches(ctx context.Context, client *river.Client[pgx.Tx], detail *metadata.MediaDetail) {
	seen := map[int]bool{}
	q := sqlc.New(w.DB)

	enqueue := func(tmdbID int) {
		if tmdbID == 0 || seen[tmdbID] {
			return
		}
		seen[tmdbID] = true

		person, err := q.GetPersonByTmdbID(ctx, pgInt4Val(int32(tmdbID)))
		if err != nil {
			return
		}
		client.Insert(ctx, PersonFetchArgs{
			PersonID: person.ID,
			TmdbID:   int32(tmdbID),
		}, nil)
	}

	for _, c := range detail.Cast {
		enqueue(c.TmdbID)
	}
	for _, c := range detail.Crew {
		enqueue(c.TmdbID)
	}
}

func pgInt4Val(v int32) pgtype.Int4 {
	return pgtype.Int4{Int32: v, Valid: true}
}
