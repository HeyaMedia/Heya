package worker

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// FetchArtworkWorker (formerly EnrichmentWorker) runs the secondary
// artwork pass — fetches the full artwork catalogue from heya.media
// and fans out DownloadImageArgs for additional backdrops + alternate
// posters/logos beyond what the primary enrich populated. See the doc
// on FetchArtworkArgs for the trigger paths.
type FetchArtworkWorker struct {
	river.WorkerDefaults[FetchArtworkArgs]
	DB       *pgxpool.Pool
	Heya     *heyamedia.HeyaProvider
	Progress *TaskProgressBroadcaster
}

func (w *FetchArtworkWorker) Work(ctx context.Context, job *river.Job[FetchArtworkArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		return nil
	}

	w.Progress.SetCurrentByKind(FetchArtworkArgs{}.Kind(), item.Title)

	var externalIDs map[string]string
	if err := decodeJSON(item.ExternalIds, &externalIDs); err != nil {
		externalIDs = make(map[string]string)
	}

	kind := metadata.MediaKind(job.Args.MediaType)

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		log.Warn().Err(err).Msg("enrichment: library not found")
		return nil
	}
	settings := metadata.ParseSettings(lib.Settings)

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	client := river.ClientFromContext[pgx.Tx](ctx)

	maxPerType := map[string]int{
		"backdrop": 5,
		"poster":   1,
		"logo":     1,
		"banner":   1,
		"art":      1,
		"thumb":    1,
		"disc":     1,
	}

	existingAssets, _ := q.ListMediaAssets(ctx, job.Args.MediaItemID)
	countPerType := map[string]int{}
	for _, a := range existingAssets {
		if a.Label == "" {
			countPerType[string(a.AssetType)]++
		}
	}

	artworks, err := w.Heya.FetchArtwork(ctx, kind, externalIDs, fetchOpts)
	if err != nil {
		log.Debug().Err(err).Msg("artwork fetch failed")
		return nil
	}

	sortOrder := 10
	for _, art := range artworks {
		if art.URL == "" {
			continue
		}
		limit := maxPerType[art.AssetType]
		if limit == 0 {
			limit = 1
		}
		if countPerType[art.AssetType] >= limit {
			continue
		}
		countPerType[art.AssetType]++
		client.Insert(ctx, DownloadImageArgs{
			MediaItemID: job.Args.MediaItemID,
			URL:         art.URL,
			AssetType:   art.AssetType,
			MediaType:   job.Args.MediaType,
			Label:       art.Language,
			SortOrder:   sortOrder,
		}, nil)
		sortOrder++
	}

	log.Debug().Int64("media_item_id", job.Args.MediaItemID).Int("artworks_queued", sortOrder-10).Msg("enrichment complete")
	return nil
}

func decodeJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
