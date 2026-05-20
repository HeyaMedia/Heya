package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type MetadataFetchWorker struct {
	river.WorkerDefaults[MetadataFetchArgs]
	DB       *pgxpool.Pool
	Matcher  *matcher.Matcher
	Registry *metadata.Registry
	Hub      *eventhub.Hub
}

func (w *MetadataFetchWorker) Work(ctx context.Context, job *river.Job[MetadataFetchArgs]) error {
	provider, ok := w.Registry.Provider(job.Args.ProviderName)
	if !ok {
		log.Warn().Str("provider", job.Args.ProviderName).Msg("provider not found in registry")
		return nil
	}

	q := sqlc.New(w.DB)
	lib, _ := q.GetLibraryByID(ctx, job.Args.LibraryID)
	settings := metadata.ParseSettings(lib.Settings)

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	detail, err := provider.GetDetail(ctx, job.Args.ProviderID, fetchOpts)
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
		}, &river.InsertOpts{Priority: 1})
	}
	if detail.BackdropURL != "" {
		client.Insert(ctx, DownloadImageArgs{
			MediaItemID: job.Args.MediaItemID,
			EntityType:  "media",
			URL:         detail.BackdropURL,
			AssetType:   "backdrop",
			MediaType:   job.Args.MediaType,
		}, &river.InsertOpts{Priority: 1})
	}

	existingAssets, _ := q.ListMediaAssets(ctx, job.Args.MediaItemID)
	hasLabel := make(map[string]bool, len(existingAssets))
	for _, a := range existingAssets {
		if a.Label != "" {
			hasLabel[a.Label] = true
		}
	}

	for _, season := range detail.Seasons {
		seasonLabel := fmt.Sprintf("season-%d", season.Number)
		if season.PosterURL != "" && !hasLabel[fmt.Sprintf("season%02d-poster", season.Number)] {
			client.Insert(ctx, DownloadImageArgs{
				MediaItemID: job.Args.MediaItemID,
				EntityType:  "media",
				URL:         season.PosterURL,
				AssetType:   "poster",
				MediaType:   job.Args.MediaType,
				Label:       seasonLabel,
				SortOrder:   1000 + season.Number,
			}, &river.InsertOpts{Priority: 2})
		}
		for _, ep := range season.Episodes {
			epLabel := fmt.Sprintf("s%02de%02d", season.Number, ep.Number)
			if ep.StillURL != "" && !hasLabel[epLabel] {
				client.Insert(ctx, DownloadImageArgs{
					MediaItemID: job.Args.MediaItemID,
					EntityType:  "media",
					URL:         ep.StillURL,
					AssetType:   "backdrop",
					MediaType:   job.Args.MediaType,
					Label:       epLabel,
					SortOrder:   2000 + season.Number*100 + ep.Number,
				}, &river.InsertOpts{Priority: 3})
			}
		}
	}

	client.Insert(ctx, EnrichmentArgs{
		MediaItemID: job.Args.MediaItemID,
		MediaType:   job.Args.MediaType,
	}, &river.InsertOpts{Priority: 3})

	w.enqueuePersonFetches(ctx, client, detail)

	if len(settings.RatingsProviders) > 0 {
		client.Insert(ctx, RatingsFetchArgs{
			MediaItemID: job.Args.MediaItemID,
			LibraryID:   job.Args.LibraryID,
		}, nil)
	}

	if settings.SaveNFO {
		client.Insert(ctx, SaveNFOArgs{
			MediaItemID:   job.Args.MediaItemID,
			LibraryFileID: job.Args.LibraryFileID,
			FilePath:      job.Args.FilePath,
			MediaType:     job.Args.MediaType,
		}, nil)
	}

	q.MarkMetadataRefreshed(ctx, job.Args.MediaItemID)

	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: job.Args.MediaItemID,
			LibraryID:   job.Args.LibraryID,
			Title:       detail.Title,
			MediaType:   job.Args.MediaType,
		})
	}

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
		}, &river.InsertOpts{Priority: 4})
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
