package worker

import (
	"context"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SaveImagesWorker struct {
	river.WorkerDefaults[SaveImagesArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *SaveImagesWorker) Work(ctx context.Context, job *river.Job[SaveImagesArgs]) error {
	if job.Args.AlbumID > 0 {
		return w.saveAlbumCover(ctx, job)
	}

	w.Progress.SetCurrentByKind(SaveImagesArgs{}.Kind(), job.Args.AssetType+" → "+filepath.Base(job.Args.FilePath))

	mediaDir := saver.MediaDir(job.Args.FilePath)

	// Music: the library_files attached to an artist item are track files
	// nested Artist/Album/track.flac — MediaDir only strips Season/Disc
	// subdirs, which would misfile artist art into whichever album the
	// sample track happens to live in. Artist art belongs in the artist
	// directory, one level above the release, behind the same write-time
	// identity check the music NFO writer uses.
	q := sqlc.New(w.DB)
	if item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID); err == nil && item.MediaType == sqlc.MediaTypeMusic {
		artistDir := filepath.Dir(releaseDirOf(job.Args.FilePath))
		artist, err := q.GetArtistByMediaItemID(ctx, item.ID)
		if err != nil || !musicArtistDirMatches(artistDir, artist) {
			log.Warn().
				Int64("media_item_id", job.Args.MediaItemID).
				Str("asset", job.Args.AssetType).
				Str("dir", artistDir).
				Msg("refusing to write artist art outside matching artist directory")
			return nil
		}
		mediaDir = artistDir
	}

	if err := saver.SaveImageToMediaDir(mediaDir, job.Args.CachedPath, job.Args.AssetType, job.Args.SortOrder); err != nil {
		log.Warn().Err(vfs.RedactError(err)).Str("asset", job.Args.AssetType).Msg("failed to save image to media dir")
	}

	return nil
}

func (w *SaveImagesWorker) saveAlbumCover(ctx context.Context, job *river.Job[SaveImagesArgs]) error {
	q := sqlc.New(w.DB)
	samplePath, err := q.GetAlbumReleaseDir(ctx, job.Args.AlbumID)
	if err != nil {
		return nil
	}
	if samplePath == "" {
		return nil // no on-disk files (soft-deleted) — nowhere to export
	}
	albumDir := releaseDirOf(samplePath)
	w.Progress.SetCurrentByKind(SaveImagesArgs{}.Kind(), "cover → "+filepath.Base(albumDir))

	if err := saver.SaveAlbumCoverToDir(albumDir, job.Args.CachedPath); err != nil {
		log.Warn().Err(vfs.RedactError(err)).Int64("album_id", job.Args.AlbumID).Str("dir", vfs.RedactPath(albumDir)).Msg("failed to save album cover to release dir")
	}
	return nil
}
