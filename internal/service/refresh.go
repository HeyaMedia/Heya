package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/worker"
)

func (a *App) RefreshMediaItem(ctx context.Context, mediaItemID int64) error {
	q := sqlc.New(a.db)
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return fmt.Errorf("media item %d not found: %w", mediaItemID, err)
	}

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		return fmt.Errorf("parsing external IDs: %w", err)
	}

	kind := mediaTypeToKind(item.MediaType)

	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return fmt.Errorf("library not found: %w", err)
	}
	settings := metadata.ParseSettings(lib.Settings)
	_ = kind

	var fetchOpts *metadata.FetchOptions
	if settings.PreferredLanguage != "" || settings.PreferredCountry != "" {
		fetchOpts = &metadata.FetchOptions{Language: settings.PreferredLanguage, Country: settings.PreferredCountry}
	}

	providerID := heyamedia.BuildLookupID(item.HeyaSlug, externalIDs)
	if providerID == "" {
		return fmt.Errorf("media item %d has no lookup identifier", mediaItemID)
	}

	detail, err := a.heya.GetDetail(ctx, providerID, fetchOpts)
	if err != nil {
		return fmt.Errorf("metadata fetch failed: %w", err)
	}

	detailJSON, _ := json.Marshal(detail.ExternalIDs)
	q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
		ID:               mediaItemID,
		Title:            detail.Title,
		SortTitle:        detail.SortTitle,
		Year:             detail.Year,
		Description:      detail.Description,
		PosterPath:       item.PosterPath,
		BackdropPath:     item.BackdropPath,
		ExternalIds:      detailJSON,
		Tagline:          item.Tagline,
		OriginalTitle:    item.OriginalTitle,
		OriginalLanguage: item.OriginalLanguage,
		Status:           item.Status,
		ProviderKind:     item.ProviderKind,
		HeyaSlug:         item.HeyaSlug,
	})

	a.river.Insert(ctx, worker.EnrichmentArgs{
		MediaItemID: mediaItemID,
		MediaType:   string(item.MediaType),
	}, nil)

	return nil
}

func mediaTypeToKind(mt sqlc.MediaType) metadata.MediaKind {
	switch mt {
	case sqlc.MediaTypeMovie:
		return metadata.KindMovie
	case sqlc.MediaTypeTv:
		return metadata.KindTV
	case sqlc.MediaTypeMusic:
		return metadata.KindMusic
	case sqlc.MediaTypeBook:
		return metadata.KindBook
	default:
		return metadata.KindMovie
	}
}
