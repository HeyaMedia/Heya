package worker

import (
	"context"
	"fmt"
	"path/filepath"

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
	Hub        EventPublisher
	Progress   *TaskProgressBroadcaster
}

func (w *DownloadImageWorker) Work(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	if job.Args.URL == "" {
		return nil
	}

	label := job.Args.AssetType
	if job.Args.Label != "" {
		label = job.Args.AssetType + " (" + job.Args.Label + ")"
	}
	w.Progress.SetCurrentByKind(DownloadImageArgs{}.Kind(), label)

	if job.Args.EntityType == "person" {
		return w.downloadPersonImage(ctx, job)
	}

	q := sqlc.New(w.DB)

	ext := filepath.Ext(job.Args.URL)
	if ext == "" {
		ext = ".jpg"
	}

	filename := job.Args.AssetType
	if job.Args.SortOrder > 0 {
		filename = fmt.Sprintf("%s%d", job.Args.AssetType, job.Args.SortOrder)
	}
	filename += ext

	dirName := fmt.Sprintf("%d", job.Args.MediaItemID)
	if item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID); err == nil && item.Slug != "" {
		dirName = item.Slug
	}

	localPath, err := w.Downloader.Download(ctx, job.Args.URL, job.Args.MediaType, dirName, filename)
	if err != nil {
		log.Warn().Err(err).
			Int64("item_id", job.Args.MediaItemID).
			Str("media_type", job.Args.MediaType).
			Str("asset_type", job.Args.AssetType).
			Str("url", job.Args.URL).
			Msg("image download failed")
		return err
	}

	if localPath == "" {
		log.Warn().
			Int64("item_id", job.Args.MediaItemID).
			Str("media_type", job.Args.MediaType).
			Str("asset_type", job.Args.AssetType).
			Str("url", job.Args.URL).
			Msg("image download returned empty path")
		return nil
	}

	if SingleAssetTypes[job.Args.AssetType] && job.Args.Label == "" {
		existing, _ := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
			MediaItemID: job.Args.MediaItemID,
			AssetType:   sqlc.AssetType(job.Args.AssetType),
		})
		for _, old := range existing {
			if old.Label == "" {
				q.DeleteMediaAsset(ctx, old.ID)
			}
		}
	}

	asset, assetErr := q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
		MediaItemID: job.Args.MediaItemID,
		AssetType:   sqlc.AssetType(job.Args.AssetType),
		Source:      "remote",
		LocalPath:   localPath,
		RemoteUrl:   job.Args.URL,
		Label:       job.Args.Label,
		SortOrder:   int32(job.Args.SortOrder),
	})
	if assetErr != nil {
		log.Debug().Err(assetErr).Str("path", localPath).Msg("failed to create media asset")
	}
	_ = asset

	if job.Args.AssetType == "poster" && job.Args.SortOrder == 0 {
		item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
		if err == nil {
			q.UpdateMediaItem(ctx, sqlc.UpdateMediaItemParams{
				ID:               item.ID,
				Title:            item.Title,
				SortTitle:        item.SortTitle,
				Year:             item.Year,
				Description:      item.Description,
				PosterPath:       localPath,
				BackdropPath:     item.BackdropPath,
				ExternalIds:      item.ExternalIds,
				Tagline:          item.Tagline,
				OriginalTitle:    item.OriginalTitle,
				OriginalLanguage: item.OriginalLanguage,
				Status:           item.Status,
				ProviderKind:     item.ProviderKind,
				HeyaSlug:         item.HeyaSlug,
			})
			log.Info().
				Int64("item_id", item.ID).
				Str("media_type", job.Args.MediaType).
				Str("local_path", localPath).
				Msg("poster_path updated")
			if w.Hub != nil {
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
				ID:               item.ID,
				Title:            item.Title,
				SortTitle:        item.SortTitle,
				Year:             item.Year,
				Description:      item.Description,
				PosterPath:       item.PosterPath,
				BackdropPath:     localPath,
				ExternalIds:      item.ExternalIds,
				Tagline:          item.Tagline,
				OriginalTitle:    item.OriginalTitle,
				OriginalLanguage: item.OriginalLanguage,
				Status:           item.Status,
				ProviderKind:     item.ProviderKind,
				HeyaSlug:         item.HeyaSlug,
			})
		}
	}

	w.maybeSaveToMediaDir(ctx, job, localPath)

	return nil
}

func (w *DownloadImageWorker) maybeSaveToMediaDir(ctx context.Context, job *river.Job[DownloadImageArgs], localPath string) {
	if job.Args.AssetType != "poster" && job.Args.AssetType != "backdrop" {
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
	personDir := fmt.Sprintf("%d", job.Args.PersonID)
	q := sqlc.New(w.DB)
	if person, err := q.GetPersonByID(ctx, job.Args.PersonID); err == nil && person.Slug != "" {
		personDir = person.Slug
	}

	localPath, err := w.Downloader.Download(ctx, job.Args.URL, "person", personDir, "profile.jpg")
	if err != nil {
		log.Warn().Err(err).Str("url", job.Args.URL).Msg("person image download failed")
		return err
	}
	if localPath == "" {
		return nil
	}
	q.UpdatePersonProfilePath(ctx, sqlc.UpdatePersonProfilePathParams{
		ID:          job.Args.PersonID,
		ProfilePath: localPath,
	})

	log.Debug().Int64("person_id", job.Args.PersonID).Str("path", localPath).Msg("person headshot downloaded")
	return nil
}
