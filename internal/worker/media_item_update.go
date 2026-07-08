package worker

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

// updateArtworkPathColumns mirrors freshly-detected art onto
// media_items.poster_path / backdrop_path so the legacy column-based lookup
// in /api/media/{id}/image/* (and the list endpoints) immediately returns
// the file without falling back to media_assets. No-op when nothing changed.
//
// Each column is written on its own targeted UPDATE (poster_path or
// backdrop_path alone), not a full-row rewrite — so a concurrent
// poster-download and backdrop-download for the same item can't stomp each
// other's column from a stale in-memory snapshot.
func updateArtworkPathColumns(ctx context.Context, q *sqlc.Queries, item sqlc.MediaItemCard, posterPath, backdropPath string) {
	if item.PosterPath != posterPath {
		if err := q.UpdateMediaItemPosterPath(ctx, sqlc.UpdateMediaItemPosterPathParams{ID: item.ID, PosterPath: posterPath}); err != nil {
			log.Debug().Err(err).Int64("item_id", item.ID).Msg("update poster_path failed")
		}
	}
	if item.BackdropPath != backdropPath {
		if err := q.UpdateMediaItemBackdropPath(ctx, sqlc.UpdateMediaItemBackdropPathParams{ID: item.ID, BackdropPath: backdropPath}); err != nil {
			log.Debug().Err(err).Int64("item_id", item.ID).Msg("update backdrop_path failed")
		}
	}
}
