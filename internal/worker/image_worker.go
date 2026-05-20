package worker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type DownloadImageWorker struct {
	river.WorkerDefaults[DownloadImageArgs]
	DB         *pgxpool.Pool
	Downloader *images.Downloader
	HeyaMedia  *heyamedia.Client
	Hub        *eventhub.Hub
}

func (w *DownloadImageWorker) Work(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	if job.Args.URL == "" {
		return nil
	}

	if w.HeyaMedia != nil && strings.HasPrefix(job.Args.URL, "http") {
		if cdnURL := w.HeyaMedia.ProxyImageURL(ctx, job.Args.URL); cdnURL != job.Args.URL {
			job.Args.URL = cdnURL
		}
	}

	if job.Args.EntityType == "person" {
		return w.downloadPersonImage(ctx, job)
	}

	q := sqlc.New(w.DB)
	if job.Args.SortOrder == 0 && (job.Args.AssetType == "poster" || job.Args.AssetType == "backdrop") {
		item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
		if err == nil {
			path := item.PosterPath
			if job.Args.AssetType == "backdrop" {
				path = item.BackdropPath
			}
			if path != "" && !strings.HasPrefix(path, "http") {
				log.Debug().Str("type", job.Args.AssetType).Str("path", path).Msg("local image exists, skipping download")
				return nil
			}
		}
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
			if w.Hub != nil && job.Args.SortOrder == 0 {
				w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
					MediaItemID: job.Args.MediaItemID,
					MediaType:   job.Args.MediaType,
				})
			}
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

	w.maybeSaveToMediaDir(ctx, job, localPath)

	return nil
}

func (w *DownloadImageWorker) maybeSaveToMediaDir(ctx context.Context, job *river.Job[DownloadImageArgs], localPath string) {
	if job.Args.AssetType != "poster" && job.Args.AssetType != "backdrop" && job.Args.AssetType != "fanart" {
		return
	}
	if job.Args.SortOrder > 0 {
		return
	}

	q := sqlc.New(w.DB)
	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		return
	}
	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return
	}
	settings := metadata.ParseSettings(lib.Settings)
	if !settings.SaveImages {
		return
	}

	files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: job.Args.MediaItemID, Valid: true})
	if err != nil || len(files) == 0 {
		return
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	client.Insert(ctx, SaveImagesArgs{
		MediaItemID: job.Args.MediaItemID,
		FilePath:    files[0].Path,
		CachedPath:  localPath,
		AssetType:   job.Args.AssetType,
	}, nil)
}

func (w *DownloadImageWorker) downloadPersonImage(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	localPath, err := w.Downloader.Download(ctx, job.Args.URL, "person", job.Args.PersonID, "profile.jpg")
	if err != nil {
		log.Warn().Err(err).Str("url", job.Args.URL).Msg("person image download failed")
		return nil
	}
	if localPath == "" {
		return nil
	}

	q := sqlc.New(w.DB)
	q.UpdatePersonProfilePath(ctx, sqlc.UpdatePersonProfilePathParams{
		ID:          job.Args.PersonID,
		ProfilePath: localPath,
	})

	log.Debug().Int64("person_id", job.Args.PersonID).Str("path", localPath).Msg("person headshot downloaded")
	return nil
}
