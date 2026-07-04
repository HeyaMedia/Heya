package worker

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// updateMediaItemParamsFrom spells every UpdateMediaItem field from the
// current row. Callers override the one or two fields they're changing —
// UpdateMediaItem is a full-row write, so any field not copied here would be
// silently blanked.
func updateMediaItemParamsFrom(item sqlc.MediaItem) sqlc.UpdateMediaItemParams {
	return sqlc.UpdateMediaItemParams{
		ID:               item.ID,
		Title:            item.Title,
		SortTitle:        item.SortTitle,
		Year:             item.Year,
		Description:      item.Description,
		PosterPath:       item.PosterPath,
		BackdropPath:     item.BackdropPath,
		ExternalIds:      item.ExternalIds,
		Tagline:          item.Tagline,
		OriginalTitle:    item.OriginalTitle,
		OriginalLanguage: item.OriginalLanguage,
		Status:           item.Status,
		ProviderKind:     item.ProviderKind,
		HeyaSlug:         item.HeyaSlug,
	}
}

// updateArtworkPathColumns mirrors freshly-detected art onto
// media_items.poster_path / backdrop_path so the legacy column-based lookup
// in /api/media/{id}/image/* (and the list endpoints) immediately returns
// the file without falling back to media_assets. No-op when nothing changed.
//
// Callers doing multiple updates against one item snapshot must advance the
// snapshot after each call (item.PosterPath = ...) — the write is full-row,
// so a stale snapshot would revert the previous update.
func updateArtworkPathColumns(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItem, posterPath, backdropPath string) {
	if item.PosterPath == posterPath && item.BackdropPath == backdropPath {
		return
	}
	p := updateMediaItemParamsFrom(item)
	p.PosterPath = posterPath
	p.BackdropPath = backdropPath
	if _, err := q.UpdateMediaItem(ctx, p); err != nil {
		log.Debug().Err(err).Int64("item_id", item.ID).Msg("update artwork path columns failed")
	}
}
