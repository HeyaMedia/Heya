package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/worker"
)

func (a *App) RefreshMediaItem(ctx context.Context, mediaItemID int64) error {
	q := sqlc.New(a.DB)
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return fmt.Errorf("media item %d not found: %w", mediaItemID, err)
	}

	var externalIDs map[string]string
	if err := json.Unmarshal(item.ExternalIds, &externalIDs); err != nil {
		return fmt.Errorf("parsing external IDs: %w", err)
	}

	kind := mediaTypeToKind(item.MediaType)

	for _, p := range a.Providers {
		if !p.Supports(kind) {
			continue
		}

		providerID := buildProviderID(p.Name(), kind, externalIDs)
		if providerID == "" {
			continue
		}

		detail, err := p.GetDetail(ctx, providerID)
		if err != nil {
			continue
		}

		detailJSON, _ := json.Marshal(detail.ExternalIDs)
		q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
			ID:           mediaItemID,
			Title:        detail.Title,
			SortTitle:    detail.SortTitle,
			Year:         detail.Year,
			Description:  detail.Description,
			PosterPath:   item.PosterPath,
			BackdropPath: item.BackdropPath,
			ExternalIds:  detailJSON,
		})

		a.River.Insert(ctx, worker.EnrichmentArgs{
			MediaItemID: mediaItemID,
			MediaType:   string(item.MediaType),
		}, nil)

		return nil
	}

	return fmt.Errorf("no provider could refresh media item %d", mediaItemID)
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

func buildProviderID(providerName string, kind metadata.MediaKind, externalIDs map[string]string) string {
	switch providerName {
	case "tmdb":
		if id := externalIDs["tmdb"]; id != "" {
			return string(kind) + ":" + id
		}
	case "musicbrainz":
		if id := externalIDs["musicbrainz"]; id != "" {
			return "musicbrainz:" + id
		}
	case "openlibrary":
		if id := externalIDs["openlibrary"]; id != "" {
			return "openlibrary:" + id
		}
	}
	return ""
}
