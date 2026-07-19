package worker

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/saver"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type SaveNFOWorker struct {
	river.WorkerDefaults[SaveNFOArgs]
	DB              *pgxpool.Pool
	Progress        *TaskProgressBroadcaster
	GeneratedWrites GeneratedWriteSuppressor
}

func (w *SaveNFOWorker) Work(ctx context.Context, job *river.Job[SaveNFOArgs]) error {
	q := sqlc.New(w.DB)

	item, err := q.GetMediaItemByID(ctx, job.Args.MediaItemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if job.Args.FilePath == "" {
		log.Debug().Int64("media_id", item.ID).Msg("save_nfo: no file path supplied, skipping")
		return nil
	}
	currentSource, err := currentNFOSource(ctx, q, item.ID, item.LibraryID, job.Args.LibraryFileID, job.Args.FilePath)
	if err != nil {
		return err
	}
	if !currentSource {
		log.Warn().
			Int64("library_file_id", job.Args.LibraryFileID).
			Int64("media_id", item.ID).
			Msg("save_nfo: queued file ownership changed, skipping stale write")
		return nil
	}
	allowed, err := generatedWriteAllowed(ctx, q, item.LibraryID, generatedWriteNFO)
	if err != nil {
		return err
	}
	if !allowed {
		log.Debug().Int64("library_id", item.LibraryID).Int64("media_id", item.ID).Msg("save_nfo: disabled before execution, skipping")
		return nil
	}

	w.Progress.SetCurrentByKind(SaveNFOArgs{}.Kind(), item.Title)

	mediaDir := saver.MediaDir(job.Args.FilePath)

	switch sqlc.MediaType(job.Args.MediaType) {
	case sqlc.MediaTypeMovie:
		movie, err := q.GetMovieByMediaItemID(ctx, item.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		allowed, err = generatedWriteAllowed(ctx, q, item.LibraryID, generatedWriteNFO)
		if err != nil {
			return err
		}
		if !allowed {
			return nil
		}
		prepared, err := saver.PrepareMovieNFO(mediaDir, item, movie)
		if err == nil {
			err = publishGeneratedWriteWhenAllowed(ctx, w.DB, w.GeneratedWrites, q, item.LibraryID, generatedWriteNFO, prepared, func(validateCtx context.Context) (bool, error) {
				return currentNFOSource(validateCtx, q, item.ID, item.LibraryID, job.Args.LibraryFileID, job.Args.FilePath)
			})
		}
		if err != nil {
			log.Warn().Err(vfs.RedactError(err)).Int64("media_id", item.ID).Msg("failed to write movie NFO")
			return err
		}

	case sqlc.MediaTypeTv, sqlc.MediaTypeAnime:
		series, err := q.GetTVSeriesByMediaItemID(ctx, item.ID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		allowed, err = generatedWriteAllowed(ctx, q, item.LibraryID, generatedWriteNFO)
		if err != nil {
			return err
		}
		if !allowed {
			return nil
		}
		prepared, err := saver.PrepareTVShowNFO(mediaDir, item, series)
		if err == nil {
			err = publishGeneratedWriteWhenAllowed(ctx, w.DB, w.GeneratedWrites, q, item.LibraryID, generatedWriteNFO, prepared, func(validateCtx context.Context) (bool, error) {
				return currentNFOSource(validateCtx, q, item.ID, item.LibraryID, job.Args.LibraryFileID, job.Args.FilePath)
			})
		}
		if err != nil {
			log.Warn().Err(vfs.RedactError(err)).Int64("media_id", item.ID).Msg("failed to write tvshow NFO")
			return err
		}
	}

	return nil
}

func currentNFOSource(ctx context.Context, q *sqlc.Queries, mediaItemID, libraryID, libraryFileID int64, path string) (bool, error) {
	file, err := q.GetLibraryFileByID(ctx, libraryFileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !file.DeletedAt.Valid &&
		file.LibraryID == libraryID &&
		file.MediaItemID.Valid &&
		file.MediaItemID.Int64 == mediaItemID &&
		filepath.Clean(file.Path) == filepath.Clean(path), nil
}
