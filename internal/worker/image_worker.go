package worker

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/database/sqlc"
	"github.com/karbowiak/kura/internal/images"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type DownloadImageWorker struct {
	river.WorkerDefaults[DownloadImageArgs]
	DB         *pgxpool.Pool
	Downloader *images.Downloader
}

func (w *DownloadImageWorker) Work(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	if job.Args.URL == "" {
		return nil
	}

	ext := filepath.Ext(job.Args.URL)
	if ext == "" {
		ext = ".jpg"
	}

	filename := job.Args.AssetType
	if job.Args.SortOrder > 0 {
		filename = fmt.Sprintf("%s%d", job.Args.AssetType, job.Args.SortOrder)
	}
	filename += ext

	localPath, err := w.Downloader.Download(ctx, job.Args.URL, job.Args.MediaType, job.Args.MediaItemID, filename)
	if err != nil {
		log.Warn().Err(err).Str("url", job.Args.URL).Msg("image download failed")
		return nil
	}

	if localPath == "" {
		return nil
	}

	q := sqlc.New(w.DB)
	q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: job.Args.MediaItemID,
		AssetType:   sqlc.AssetType(job.Args.AssetType),
		Source:      "remote",
		LocalPath:   localPath,
		RemoteUrl:   job.Args.URL,
		Label:       job.Args.Label,
		SortOrder:   int32(job.Args.SortOrder),
	})

	if job.Args.AssetType == "poster" {
		item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
		if err == nil {
			q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
				ID:           item.ID,
				Title:        item.Title,
				SortTitle:    item.SortTitle,
				Year:         item.Year,
				Description:  item.Description,
				PosterPath:   localPath,
				BackdropPath: item.BackdropPath,
				ExternalIds:  item.ExternalIds,
			})
		}
	}

	if job.Args.AssetType == "backdrop" && job.Args.SortOrder == 0 {
		item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
		if err == nil {
			q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
				ID:           item.ID,
				Title:        item.Title,
				SortTitle:    item.SortTitle,
				Year:         item.Year,
				Description:  item.Description,
				PosterPath:   item.PosterPath,
				BackdropPath: localPath,
				ExternalIds:  item.ExternalIds,
			})
		}
	}

	return nil
}
