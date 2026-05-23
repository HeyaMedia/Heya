package service

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
)

func (a *App) UpdateWatchProgress(ctx context.Context, userID int64, entityType string, entityID int64, progress, total int32) (sqlc.UserWatchProgress, error) {
	if entityType == "" {
		entityType = "movie"
	}

	completed := total > 0 && progress >= total-30

	if a.hub != nil {
		a.hub.Emit(eventhub.EventMediaWatched, eventhub.WatchPayload{
			UserID:      userID,
			MediaItemID: entityID,
			Progress:    progress,
			Total:       total,
			Completed:   completed,
		})
	}

	q := sqlc.New(a.db)
	return q.UpsertWatchProgress(ctx, sqlc.UpsertWatchProgressParams{
		UserID:          userID,
		EntityType:      entityType,
		EntityID:        entityID,
		ProgressSeconds: progress,
		TotalSeconds:    total,
		Completed:       completed,
	})
}

func (a *App) ListContinueWatching(ctx context.Context, userID int64) ([]sqlc.ListContinueWatchingRow, error) {
	q := sqlc.New(a.db)
	return q.ListContinueWatching(ctx, userID)
}

func (a *App) ListRecentlyWatched(ctx context.Context, userID int64) ([]sqlc.ListRecentlyWatchedRow, error) {
	q := sqlc.New(a.db)
	return q.ListRecentlyWatched(ctx, userID)
}

func (a *App) ToggleFavorite(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	_, err := q.ToggleFavorite(ctx, sqlc.ToggleFavoriteParams{
		UserID:     userID,
		EntityType: "media_item",
		EntityID:   mediaItemID,
	})
	return err
}

func (a *App) IsFavorited(ctx context.Context, userID, mediaItemID int64) (bool, error) {
	q := sqlc.New(a.db)
	return q.IsFavorited(ctx, sqlc.IsFavoritedParams{
		UserID:     userID,
		EntityType: "media_item",
		EntityID:   mediaItemID,
	})
}
