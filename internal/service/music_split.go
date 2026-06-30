package service

import (
	"context"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
)

// SplitArtist moves the albums of artistID that live under `folder` into their
// own artist row — repairing an over-eager enrichment merge (e.g. an "Avicii"
// folder that got fused into "Alicia Keys") — then queues the new artist for
// re-enrichment so it picks up correct metadata under the current matching
// gates. Returns the split summary; AlbumsMoved == 0 means nothing lived under
// that folder.
func (a *App) SplitArtist(ctx context.Context, artistID int64, folder string) (matcher.SplitArtistResult, error) {
	res, err := a.matcher.SplitArtistByFolder(ctx, artistID, folder)
	if err != nil {
		return res, err
	}
	if res.AlbumsMoved > 0 {
		if enqErr := worker.EnqueueEnrich(ctx, a.river, res.NewArtistMediaItem, sqlc.MediaTypeMusic, worker.EnrichSourceForced); enqErr != nil {
			log.Warn().Err(enqErr).
				Int64("media_item", res.NewArtistMediaItem).
				Msg("split-artist: enqueue re-enrich failed (will be picked up by the next scan)")
		}
	}
	return res, nil
}
