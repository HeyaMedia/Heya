package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type MetadataFetchWorker struct {
	river.WorkerDefaults[MetadataFetchArgs]
	DB      *pgxpool.Pool
	Matcher MatchService
	Heya    *heyamedia.HeyaProvider
	Hub     EventPublisher
}

func (w *MetadataFetchWorker) Work(ctx context.Context, job *river.Job[MetadataFetchArgs]) error {
	q := sqlc.New(w.DB)
	lib, _ := q.GetLibraryByID(ctx, job.Args.LibraryID)
	settings := metadata.ParseSettings(lib.Settings)

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	detail, err := w.Heya.GetDetail(ctx, job.Args.ProviderID, fetchOpts)
	if err != nil {
		log.Warn().Err(err).Str("id", job.Args.ProviderID).Msg("metadata fetch failed")
		return nil
	}

	mediaType := sqlc.MediaType(job.Args.MediaType)
	kind := matcher.MediaTypeToKind(mediaType)

	w.Matcher.StoreEntityMetadata(ctx, job.Args.MediaItemID, kind, detail)
	w.Matcher.StoreRichMetadata(ctx, job.Args.MediaItemID, detail)

	client := river.ClientFromContext[pgx.Tx](ctx)

	var pending []PendingImage
	if detail.PosterURL != "" {
		pending = append(pending, PendingImage{URL: detail.PosterURL, AssetType: "poster", SortOrder: 0, Priority: 1})
	}
	if detail.BackdropURL != "" {
		pending = append(pending, PendingImage{URL: detail.BackdropURL, AssetType: "backdrop", SortOrder: 0, Priority: 1})
	}
	for _, season := range detail.Seasons {
		if season.PosterURL != "" {
			pending = append(pending, PendingImage{
				URL: season.PosterURL, AssetType: "poster",
				Label: fmt.Sprintf("season-%d", season.Number), SortOrder: 1000 + season.Number, Priority: 2,
			})
		}
		for _, ep := range season.Episodes {
			if ep.StillURL != "" {
				pending = append(pending, PendingImage{
					URL: ep.StillURL, AssetType: "backdrop",
					Label: fmt.Sprintf("s%02de%02d", season.Number, ep.Number), SortOrder: 2000 + season.Number*100 + ep.Number, Priority: 3,
				})
			}
		}
	}

	client.Insert(ctx, DetectLocalAssetsArgs{
		MediaItemID:   job.Args.MediaItemID,
		LibraryFileID: job.Args.LibraryFileID,
		FilePath:      job.Args.FilePath,
		MediaType:     job.Args.MediaType,
		PendingImages: pending,
		QueueEnrich:   true,
		LibraryID:     job.Args.LibraryID,
	}, nil)

	w.enqueuePersonFetches(ctx, client, detail, settings.PreferredLanguage)

	if settings.FetchRatings {
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

func (w *MetadataFetchWorker) enqueuePersonFetches(ctx context.Context, client *river.Client[pgx.Tx], detail *metadata.MediaDetail, lang string) {
	seen := map[int32]bool{}
	q := sqlc.New(w.DB)

	enqueue := func(ids map[string]string) {
		tmdbStr := ids["tmdb"]
		if tmdbStr == "" {
			return
		}
		n, err := strconv.ParseInt(tmdbStr, 10, 32)
		if err != nil || n == 0 {
			return
		}
		tmdbID := int32(n)
		if seen[tmdbID] {
			return
		}
		seen[tmdbID] = true

		extJSON, _ := json.Marshal(map[string]string{"tmdb": fmt.Sprintf("%d", tmdbID)})
		person, err := q.FindPersonByExternalID(ctx, extJSON)
		if err != nil {
			return
		}
		client.Insert(ctx, PersonFetchArgs{
			PersonID: person.ID,
			TmdbID:   tmdbID,
			Language: lang,
		}, &river.InsertOpts{Priority: 4})
	}

	for _, c := range detail.Cast {
		enqueue(c.ExternalIDs)
	}
	for _, c := range detail.Crew {
		enqueue(c.ExternalIDs)
	}
}
