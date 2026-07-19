package worker

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SaveImagesWorker struct {
	river.WorkerDefaults[SaveImagesArgs]
	DB              *pgxpool.Pool
	Progress        *TaskProgressBroadcaster
	GeneratedWrites GeneratedWriteSuppressor
}

func (w *SaveImagesWorker) Work(ctx context.Context, job *river.Job[SaveImagesArgs]) error {
	q := sqlc.New(w.DB)
	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	allowed, err := generatedWriteAllowed(ctx, q, item.LibraryID, generatedWriteImage)
	if err != nil {
		return err
	}
	if !allowed {
		log.Debug().Int64("library_id", item.LibraryID).Int64("media_item_id", item.ID).Msg("save_images: disabled before execution, skipping")
		return nil
	}

	if job.Args.AlbumID > 0 {
		return w.saveAlbumCover(ctx, job)
	}
	currentPath, err := currentMediaItemFilePath(ctx, q, item.ID, job.Args.FilePath)
	if err != nil {
		return err
	}
	if !currentPath {
		log.Warn().
			Int64("media_item_id", item.ID).
			Str("path", vfs.RedactPath(job.Args.FilePath)).
			Msg("save_images: queued file ownership changed, skipping stale write")
		return nil
	}
	currentSource, err := currentImageSource(ctx, q, item.ID, job.Args.AssetType, job.Args.SortOrder, job.Args.Label, job.Args.CachedPath)
	if err != nil {
		return err
	}
	if !currentSource {
		log.Warn().
			Int64("media_item_id", item.ID).
			Str("asset", job.Args.AssetType).
			Msg("save_images: queued image is no longer the current asset, skipping stale write")
		return nil
	}

	w.Progress.SetCurrentByKind(SaveImagesArgs{}.Kind(), job.Args.AssetType+" → "+filepath.Base(job.Args.FilePath))

	mediaDir := saver.MediaDir(job.Args.FilePath)

	// Music: the library_files attached to an artist item are track files
	// nested Artist/Album/track.flac — MediaDir only strips Season/Disc
	// subdirs, which would misfile artist art into whichever album the
	// sample track happens to live in. Artist art belongs in the artist
	// directory, one level above the release, behind the same write-time
	// identity check the music NFO writer uses.
	if item.MediaType == sqlc.MediaTypeMusic {
		artistDir := filepath.Dir(releaseDirOf(job.Args.FilePath))
		artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			return nil
		}
		if !musicArtistDirMatches(artistDir, artist) {
			log.Warn().
				Int64("media_item_id", job.Args.MediaItemID).
				Str("asset", job.Args.AssetType).
				Str("dir", artistDir).
				Msg("refusing to write artist art outside matching artist directory")
			return nil
		}
		mediaDir = artistDir
	}

	_, ok := saver.ImageSidecarPath(mediaDir, job.Args.CachedPath, job.Args.AssetType, job.Args.SortOrder)
	if !ok {
		return nil
	}
	allowed, err = generatedWriteAllowed(ctx, q, item.LibraryID, generatedWriteImage)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	prepared, err := saver.PrepareImageToMediaDir(mediaDir, job.Args.CachedPath, job.Args.AssetType, job.Args.SortOrder)
	if err == nil {
		err = publishGeneratedWriteWhenAllowed(ctx, w.DB, w.GeneratedWrites, q, item.LibraryID, generatedWriteImage, prepared, func(validateCtx context.Context) (bool, error) {
			return genericImageTargetCurrent(validateCtx, q, item.ID, job.Args.FilePath, job.Args.AssetType, job.Args.SortOrder, job.Args.Label, job.Args.CachedPath)
		})
	}
	if err != nil {
		log.Warn().Err(vfs.RedactError(err)).Str("asset", job.Args.AssetType).Msg("failed to save image to media dir")
		return err
	}

	return nil
}

func (w *SaveImagesWorker) saveAlbumCover(ctx context.Context, job *river.Job[SaveImagesArgs]) error {
	q := sqlc.New(w.DB)
	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if item.MediaType != sqlc.MediaTypeMusic {
		return nil
	}
	artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	album, err := q.GetAlbumByID(ctx, job.Args.AlbumID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if album.ArtistID != artist.ID {
		log.Warn().
			Int64("media_item_id", item.ID).
			Int64("album_id", album.ID).
			Msg("save_images: album no longer belongs to queued artist, skipping stale cover write")
		return nil
	}
	if !sameCachedImagePath(album.CoverPath, job.Args.CachedPath) {
		log.Warn().
			Int64("media_item_id", item.ID).
			Int64("album_id", album.ID).
			Msg("save_images: queued cover is no longer the album's current image, skipping stale write")
		return nil
	}
	samplePath, err := q.GetAlbumReleaseDir(ctx, job.Args.AlbumID)
	if err != nil {
		return err
	}
	if samplePath == "" {
		return nil // no on-disk files (soft-deleted) — nowhere to export
	}
	albumDir := releaseDirOf(samplePath)
	w.Progress.SetCurrentByKind(SaveImagesArgs{}.Kind(), "cover → "+filepath.Base(albumDir))

	allowed, err := generatedWriteAllowed(ctx, q, item.LibraryID, generatedWriteImage)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	prepared, err := saver.PrepareAlbumCoverToDir(albumDir, job.Args.CachedPath)
	if err == nil {
		err = publishGeneratedWriteWhenAllowed(ctx, w.DB, w.GeneratedWrites, q, item.LibraryID, generatedWriteImage, prepared, func(validateCtx context.Context) (bool, error) {
			return albumCoverPublicationCurrent(validateCtx, q, item.ID, album.ID, albumDir, job.Args.CachedPath)
		})
	}
	if err != nil {
		log.Warn().Err(vfs.RedactError(err)).Int64("album_id", job.Args.AlbumID).Str("dir", vfs.RedactPath(albumDir)).Msg("failed to save album cover to release dir")
		return err
	}
	return nil
}

func albumCoverTargetCurrent(ctx context.Context, q *sqlc.Queries, mediaItemID, albumID int64, expectedAlbumDir string) (bool, error) {
	artist, err := q.GetArtistByMediaItemID(ctx, mediaItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	album, err := q.GetAlbumByID(ctx, albumID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if album.ArtistID != artist.ID {
		return false, nil
	}
	currentPath, err := q.GetAlbumReleaseDir(ctx, albumID)
	if err != nil {
		return false, err
	}
	return currentPath != "" && filepath.Clean(releaseDirOf(currentPath)) == filepath.Clean(expectedAlbumDir), nil
}

func albumCoverPublicationCurrent(ctx context.Context, q *sqlc.Queries, mediaItemID, albumID int64, expectedAlbumDir, cachedPath string) (bool, error) {
	current, err := albumCoverTargetCurrent(ctx, q, mediaItemID, albumID, expectedAlbumDir)
	if err != nil || !current {
		return current, err
	}
	album, err := q.GetAlbumByID(ctx, albumID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sameCachedImagePath(album.CoverPath, cachedPath), nil
}

func genericImageTargetCurrent(ctx context.Context, q *sqlc.Queries, mediaItemID int64, filePath, assetType string, sortOrder int, label, cachedPath string) (bool, error) {
	current, err := currentMediaItemFilePath(ctx, q, mediaItemID, filePath)
	if err != nil || !current {
		return current, err
	}
	return currentImageSource(ctx, q, mediaItemID, assetType, sortOrder, label, cachedPath)
}

func currentImageSource(ctx context.Context, q *sqlc.Queries, mediaItemID int64, assetType string, sortOrder int, label, cachedPath string) (bool, error) {
	assets, err := q.ListMediaAssetsByType(ctx, sqlc.ListMediaAssetsByTypeParams{
		MediaItemID: mediaItemID,
		AssetType:   sqlc.AssetType(assetType),
	})
	if err != nil {
		return false, err
	}
	for _, asset := range assets {
		if int(asset.SortOrder) == sortOrder && asset.Label == label && sameCachedImagePath(asset.LocalPath, cachedPath) {
			return true, nil
		}
	}

	// Upgraded primary poster/backdrop rows may still live solely in the legacy
	// media_items columns. Accept only the exact current local path; a queued
	// predecessor must not be exported after metadata refresh replaces it.
	if sortOrder == 0 && label == "" && (assetType == "poster" || assetType == "backdrop") {
		item, itemErr := q.GetMediaItemByID(ctx, mediaItemID)
		if errors.Is(itemErr, pgx.ErrNoRows) {
			return false, nil
		}
		if itemErr != nil {
			return false, itemErr
		}
		currentPath := item.PosterPath
		if assetType == "backdrop" {
			currentPath = item.BackdropPath
		}
		return sameCachedImagePath(currentPath, cachedPath), nil
	}
	return false, nil
}

func sameCachedImagePath(left, right string) bool {
	return left != "" && right != "" && filepath.Clean(left) == filepath.Clean(right)
}

func currentMediaItemFilePath(ctx context.Context, q *sqlc.Queries, mediaItemID int64, path string) (bool, error) {
	files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
	if err != nil {
		return false, err
	}
	want := filepath.Clean(path)
	for _, file := range files {
		if filepath.Clean(file.Path) == want {
			return true, nil
		}
	}
	return false, nil
}
