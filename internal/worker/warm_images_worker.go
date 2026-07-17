package worker

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// WarmPendingImagesWorker backfills artwork bytes for rows that predate
// eager warming (or whose downloads failed transiently): every visual
// media_assets row with a remote_url and no local_path, plus every album
// still pointing at an upstream cover URL. It only ENQUEUES download_image
// jobs — the DownloadImageWorker owns the actual fetch, dedup, and sidecar
// export — so a full-library sweep is one fast paged read. Duplicate
// enqueues against in-flight downloads coalesce via DownloadImageArgs'
// unique-while-active opts, and permanently missing images delete their
// pending row (see materializePendingAsset), so successive sweeps converge
// to a no-op.
type WarmPendingImagesWorker struct {
	river.WorkerDefaults[WarmPendingImagesArgs]
	DB *pgxpool.Pool
}

const warmSweepPageSize = 500

func (w *WarmPendingImagesWorker) Work(ctx context.Context, job *river.Job[WarmPendingImagesArgs]) error {
	q := sqlc.New(w.DB)
	client := river.ClientFromContext[pgx.Tx](ctx)

	assets := 0
	var cursor int64
	for {
		rows, err := q.ListPendingRemoteMediaAssets(ctx, sqlc.ListPendingRemoteMediaAssetsParams{
			ID:    cursor,
			Limit: warmSweepPageSize,
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			cursor = row.ID
			if _, err := client.Insert(ctx, DownloadImageArgs{
				MediaItemID: row.MediaItemID,
				EntityType:  "media",
				AssetID:     row.ID,
				URL:         row.RemoteUrl,
				AssetType:   string(row.AssetType),
				MediaType:   string(row.MediaType),
				Label:       row.Label,
				SortOrder:   int(row.SortOrder),
			}, &river.InsertOpts{Priority: PriorityAnalysis}); err != nil {
				return err
			}
			assets++
		}
	}

	covers := 0
	cursor = 0
	for {
		rows, err := q.ListAlbumsWithRemoteCovers(ctx, sqlc.ListAlbumsWithRemoteCoversParams{
			ID:    cursor,
			Limit: warmSweepPageSize,
		})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			cursor = row.ID
			if _, err := client.Insert(ctx, DownloadImageArgs{
				MediaItemID: row.MediaItemID,
				EntityType:  "album",
				AlbumID:     row.ID,
				URL:         row.CoverPath,
				AssetType:   "cover",
				MediaType:   string(sqlc.MediaTypeMusic),
			}, &river.InsertOpts{Priority: PriorityAnalysis}); err != nil {
				return err
			}
			covers++
		}
	}

	if assets > 0 || covers > 0 {
		log.Info().Int("assets", assets).Int("album_covers", covers).Msg("warm images: pending artwork downloads queued")
	}
	return nil
}
