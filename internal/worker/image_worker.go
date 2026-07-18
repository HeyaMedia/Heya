package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/images"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type DownloadImageWorker struct {
	river.WorkerDefaults[DownloadImageArgs]
	DB         *pgxpool.Pool
	Downloader *images.Downloader
	Hub        EventPublisher
	Progress   *TaskProgressBroadcaster
}

// Timeout disables River's one-minute job deadline. HeyaMetadata image
// routes intentionally return 202 while a separate materialization job waits
// on provider rate limits; the downloader follows Retry-After until bytes are
// ready or the worker is explicitly cancelled.
func (w *DownloadImageWorker) Timeout(*river.Job[DownloadImageArgs]) time.Duration {
	return -1
}

func (w *DownloadImageWorker) Work(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	if job.Args.URL == "" && job.Args.AssetID == 0 && job.Args.AlbumID == 0 {
		log.Debug().Int64("item_id", job.Args.MediaItemID).Str("asset_type", job.Args.AssetType).Msg("image: empty url, skipping")
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
	if job.Args.AlbumID > 0 {
		return w.downloadAlbumCover(ctx, job)
	}
	if job.Args.AssetID > 0 {
		return w.materializePendingAsset(ctx, job)
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

	var (
		localPath string
		err       error
	)
	if job.Args.ReplacePrimary {
		localPath, err = w.Downloader.DownloadFresh(ctx, job.Args.URL, job.Args.MediaType, dirName, filename)
	} else {
		localPath, err = w.Downloader.Download(ctx, job.Args.URL, job.Args.MediaType, dirName, filename)
	}
	if err != nil {
		if imageUnavailable(err) {
			// Upstream has no such image — expected for the bulk of episode
			// stills. Don't retry (it won't appear) and don't warn-spam.
			log.Debug().
				Int64("item_id", job.Args.MediaItemID).
				Str("media_type", job.Args.MediaType).
				Str("asset_type", job.Args.AssetType).
				Str("url", job.Args.URL).
				Msg("image unavailable upstream")
			return nil
		}
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

	var (
		storedAsset sqlc.MediaAsset
		assetErr    error
	)
	if SingleAssetTypes[job.Args.AssetType] && job.Args.Label == "" {
		if job.Args.ReplacePrimary {
			storedAsset, assetErr = q.ReplacePrimaryMediaAsset(ctx, sqlc.ReplacePrimaryMediaAssetParams{
				MediaItemID: job.Args.MediaItemID,
				AssetType:   sqlc.AssetType(job.Args.AssetType),
				Source:      "remote",
				LocalPath:   localPath,
				RemoteUrl:   job.Args.URL,
			})
		} else {
			storedAsset, assetErr = q.UpsertPrimaryMediaAsset(ctx, sqlc.UpsertPrimaryMediaAssetParams{
				MediaItemID: job.Args.MediaItemID,
				AssetType:   sqlc.AssetType(job.Args.AssetType),
				Source:      "remote",
				LocalPath:   localPath,
				RemoteUrl:   job.Args.URL,
			})
		}
	} else {
		sortOrder := job.Args.SortOrder
		if job.Args.Label != "" {
			_ = q.DeleteMediaAssetsByTypeLabel(ctx, sqlc.DeleteMediaAssetsByTypeLabelParams{
				MediaItemID: job.Args.MediaItemID,
				AssetType:   sqlc.AssetType(job.Args.AssetType),
				Label:       job.Args.Label,
			})
			sortOrder = 0
		}
		storedAsset, assetErr = q.CreateMediaAsset(ctx, sqlc.CreateMediaAssetParams{
			MediaItemID: job.Args.MediaItemID,
			AssetType:   sqlc.AssetType(job.Args.AssetType),
			Source:      "remote",
			LocalPath:   localPath,
			RemoteUrl:   job.Args.URL,
			Label:       job.Args.Label,
			SortOrder:   int32(sortOrder),
		})
	}
	if assetErr != nil {
		if !errors.Is(assetErr, pgx.ErrNoRows) {
			log.Debug().Err(assetErr).Str("path", localPath).Msg("failed to create media asset")
		}
	}
	if assetErr == nil {
		var deduped bool
		storedAsset, deduped, assetErr = MaterializeMediaAsset(ctx, w.DB, storedAsset, localPath, filepath.Join(w.Downloader.CacheDir(), "images"))
		if assetErr != nil {
			return fmt.Errorf("fingerprint downloaded %s: %w", job.Args.AssetType, assetErr)
		}
		localPath = storedAsset.LocalPath
		if deduped {
			log.Info().
				Int64("item_id", job.Args.MediaItemID).
				Str("asset_type", job.Args.AssetType).
				Int64("kept_asset_id", storedAsset.ID).
				Msg("deduplicated materialized artwork")
		}
	}

	if assetErr == nil && job.Args.AssetType == "poster" && job.Args.SortOrder == 0 {
		item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
		if err == nil {
			updateArtworkPathColumns(ctx, q, item, localPath, item.BackdropPath)
			log.Info().
				Int64("item_id", item.ID).
				Str("media_type", job.Args.MediaType).
				Str("local_path", localPath).
				Msg("poster_path updated")
		}
	}

	if job.Args.AssetType == "backdrop" && job.Args.SortOrder == 0 {
		item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
		if err == nil {
			updateArtworkPathColumns(ctx, q, item, item.PosterPath, localPath)
		}
	}

	// Store-time completion signal for ANY artwork that actually landed —
	// primary poster/backdrop AND secondary/alternate art (extra posters,
	// backdrops, banners, logos, stills). Fires once per successful store; the
	// FE coalesces bursts (useLiveRefresh's 4s window) during a scan fan-out.
	if assetErr == nil && w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: job.Args.MediaItemID,
			MediaType:   job.Args.MediaType,
		})
	}

	w.maybeSaveToMediaDir(ctx, job, localPath)

	return nil
}

func (w *DownloadImageWorker) maybeSaveToMediaDir(ctx context.Context, job *river.Job[DownloadImageArgs], localPath string) {
	w.maybeSaveToMediaDirFor(ctx, job.Args.MediaItemID, job.Args.AssetType, job.Args.SortOrder, job.Args.Label, localPath)
}

func (w *DownloadImageWorker) maybeSaveToMediaDirFor(ctx context.Context, mediaItemID int64, assetType string, sortOrder int, label, localPath string) {
	if !ShouldSaveImageSidecar(assetType, sortOrder, label) {
		return
	}

	q := sqlc.New(w.DB)
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
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

	files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
	if err != nil || len(files) == 0 {
		return
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	if _, err := client.Insert(ctx, SaveImagesArgs{
		MediaItemID: mediaItemID,
		FilePath:    files[0].Path,
		CachedPath:  localPath,
		AssetType:   assetType,
		SortOrder:   sortOrder,
		Label:       label,
	}, nil); err != nil {
		log.Debug().Err(err).Int64("media_item_id", mediaItemID).Msg("enqueue image sidecar save failed")
	}
}

// materializePendingAsset is the eager-warm path: the media_assets row
// already exists (pending, remote_url only) and this job's sole task is to
// land the bytes onto it — the exact update-in-place recipe the on-demand
// serve path uses (service.GetMediaImagePath), so the two stay
// interchangeable. Creating rows here instead would duplicate label-less
// backdrop slots and clobber whole "extra" label groups.
func (w *DownloadImageWorker) materializePendingAsset(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	q := sqlc.New(w.DB)
	asset, err := q.GetMediaAssetByID(ctx, job.Args.AssetID)
	if err != nil {
		// Row deleted or replaced since enqueue (re-enrich, dedup, user edit).
		return nil
	}
	if asset.LocalPath != "" {
		return nil // already materialized — sidecar detection or a first view won
	}
	url := asset.RemoteUrl
	if url == "" {
		url = job.Args.URL
	}
	if url == "" {
		return nil
	}

	item, err := q.GetMediaItemByID(ctx, asset.MediaItemID)
	if err != nil {
		return nil
	}
	dirName := strconv.FormatInt(item.ID, 10)
	if item.Slug != "" {
		dirName = item.Slug
	}
	// Same cache filename the on-demand path derives (imageCacheFilename):
	// "<assetType>[<sortOrder>].<ext>" — shared slot, so whichever path runs
	// first turns the other into a cheap stat.
	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".jpg"
	}
	filename := string(asset.AssetType)
	if asset.SortOrder > 0 {
		filename = fmt.Sprintf("%s%d", asset.AssetType, asset.SortOrder)
	}
	filename += ext

	localPath, err := w.Downloader.Download(ctx, url, string(item.MediaType), dirName, filename)
	if err != nil {
		if imageUnavailable(err) {
			// Upstream says the image doesn't exist. Drop the dead pointer so
			// first views stop stalling on it and warm sweeps converge; a
			// later refresh re-records the URL if it ever reappears.
			_ = q.DeleteMediaAsset(ctx, asset.ID)
			log.Debug().
				Int64("item_id", asset.MediaItemID).
				Int64("asset_id", asset.ID).
				Str("asset_type", string(asset.AssetType)).
				Str("url", url).
				Msg("image unavailable upstream, pending row dropped")
			return nil
		}
		log.Warn().Err(err).
			Int64("item_id", asset.MediaItemID).
			Int64("asset_id", asset.ID).
			Str("asset_type", string(asset.AssetType)).
			Str("url", url).
			Msg("image warm download failed")
		return err
	}
	if localPath == "" {
		return nil
	}

	representative, deduped, err := MaterializeMediaAsset(ctx, w.DB, asset, localPath, filepath.Join(w.Downloader.CacheDir(), "images"))
	if err != nil {
		log.Debug().Err(err).Int64("asset_id", asset.ID).Msg("image warm: fingerprint failed")
		if updateErr := q.UpdateMediaAssetLocalPath(ctx, sqlc.UpdateMediaAssetLocalPathParams{
			ID: asset.ID, LocalPath: localPath,
		}); updateErr != nil {
			log.Debug().Err(updateErr).Int64("asset_id", asset.ID).Msg("image warm: update local path failed")
		}
	} else {
		localPath = representative.LocalPath
		if deduped {
			log.Debug().Int64("item_id", asset.MediaItemID).Int64("kept_asset_id", representative.ID).Msg("image warm: deduplicated against existing artwork")
		}
	}

	if asset.Label == "" && asset.SortOrder == 0 {
		switch asset.AssetType {
		case sqlc.AssetTypePoster:
			updateArtworkPathColumns(ctx, q, item, localPath, item.BackdropPath)
		case sqlc.AssetTypeBackdrop:
			updateArtworkPathColumns(ctx, q, item, item.PosterPath, localPath)
		}
	}

	if w.Hub != nil {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: item.ID,
			MediaType:   string(item.MediaType),
		})
	}

	w.maybeSaveToMediaDirFor(ctx, item.ID, string(asset.AssetType), int(asset.SortOrder), asset.Label, localPath)
	return nil
}

// downloadAlbumCover warms albums.cover_path — albums aren't media items, so
// their art is a bare column, not a media_assets row. Mirrors
// service.GetAlbumCover's cache slot ("music"/album-<id>/cover.<ext>) so the
// lazy path and the warm path share bytes.
func (w *DownloadImageWorker) downloadAlbumCover(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	q := sqlc.New(w.DB)
	album, err := q.GetAlbumByID(ctx, job.Args.AlbumID)
	if err != nil {
		return nil
	}
	if !strings.HasPrefix(album.CoverPath, "http://") && !strings.HasPrefix(album.CoverPath, "https://") {
		return nil // already local — sidecar detection, embedded art, or an earlier warm won
	}

	url := album.CoverPath
	ext := filepath.Ext(url)
	if ext == "" {
		ext = ".jpg"
	}
	dirName := "album-" + strconv.FormatInt(album.ID, 10)
	localPath, err := w.Downloader.Download(ctx, url, "music", dirName, "cover"+ext)
	if err != nil {
		if imageUnavailable(err) {
			// Keep the URL: album covers on heya.media materialize behind 202s
			// and can appear later; the lazy path (and the next sweep) retries.
			log.Debug().Int64("album_id", album.ID).Str("url", url).Msg("album cover unavailable upstream")
			return nil
		}
		log.Warn().Err(err).Int64("album_id", album.ID).Str("url", url).Msg("album cover warm download failed")
		return err
	}
	if localPath == "" {
		return nil
	}

	if err := q.UpdateAlbumCoverPath(ctx, sqlc.UpdateAlbumCoverPathParams{ID: album.ID, CoverPath: localPath}); err != nil {
		log.Debug().Err(err).Int64("album_id", album.ID).Msg("album cover warm: update cover path failed")
	}

	if w.Hub != nil && job.Args.MediaItemID > 0 {
		w.Hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{
			MediaItemID: job.Args.MediaItemID,
			MediaType:   string(sqlc.MediaTypeMusic),
		})
	}

	w.maybeSaveAlbumCoverSidecar(ctx, album, localPath)
	return nil
}

// maybeSaveAlbumCoverSidecar queues a cover.<ext> export into the album's
// release directory for save_images libraries — the album analogue of
// maybeSaveToMediaDir.
func (w *DownloadImageWorker) maybeSaveAlbumCoverSidecar(ctx context.Context, album sqlc.Album, localPath string) {
	if localPath == "" {
		return
	}
	q := sqlc.New(w.DB)
	artist, err := q.GetArtistByID(ctx, album.ArtistID)
	if err != nil {
		return
	}
	item, err := q.GetMediaItemByID(ctx, artist.MediaItemID)
	if err != nil {
		return
	}
	lib, err := q.GetLibraryByID(ctx, item.LibraryID)
	if err != nil {
		return
	}
	if !metadata.ParseSettings(lib.Settings).SaveImages {
		return
	}
	client := river.ClientFromContext[pgx.Tx](ctx)
	if _, err := client.Insert(ctx, SaveImagesArgs{
		MediaItemID: item.ID,
		AlbumID:     album.ID,
		CachedPath:  localPath,
		AssetType:   "cover",
	}, nil); err != nil {
		log.Debug().Err(err).Int64("album_id", album.ID).Msg("enqueue album cover sidecar failed")
	}
}

func (w *DownloadImageWorker) downloadPersonImage(ctx context.Context, job *river.Job[DownloadImageArgs]) error {
	personDir := fmt.Sprintf("%d", job.Args.PersonID)
	q := sqlc.New(w.DB)
	if person, err := q.GetPersonByID(ctx, job.Args.PersonID); err == nil && person.Slug != "" {
		personDir = person.Slug
	}

	localPath, err := w.Downloader.Download(ctx, job.Args.URL, "person", personDir, "profile.jpg")
	if err != nil {
		if imageUnavailable(err) {
			log.Debug().Int64("person_id", job.Args.PersonID).Str("url", job.Args.URL).Msg("person image unavailable upstream")
			return nil
		}
		log.Warn().Err(err).Str("url", job.Args.URL).Msg("person image download failed")
		return err
	}
	if localPath == "" {
		return nil
	}
	if err := q.UpdatePersonProfilePath(ctx, sqlc.UpdatePersonProfilePathParams{
		ID:          job.Args.PersonID,
		ProfilePath: localPath,
	}); err != nil {
		return fmt.Errorf("store downloaded person profile path: %w", err)
	}

	log.Debug().Int64("person_id", job.Args.PersonID).Str("path", localPath).Msg("person headshot downloaded")
	return nil
}

// imageUnavailable reports whether a download error means the image simply
// isn't there upstream (a permanent 4xx) rather than a transient failure worth
// retrying. heya.media routinely advertises episode-still and headshot URLs it
// can't serve, so these 404s are expected and must not trigger River retries.
func imageUnavailable(err error) bool {
	var se *images.StatusError
	return errors.As(err, &se) && se.Permanent()
}
