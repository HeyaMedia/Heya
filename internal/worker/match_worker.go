package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type MetadataMatchWorker struct {
	river.WorkerDefaults[MetadataMatchArgs]
	DB        *pgxpool.Pool
	Matcher   *matcher.Matcher
	Providers []metadata.Provider
}

func (w *MetadataMatchWorker) Work(ctx context.Context, job *river.Job[MetadataMatchArgs]) error {
	q := sqlc.New(w.DB)

	file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		return err
	}

	if file.Status == sqlc.FileStatusMatched {
		return nil
	}

	var parsed parser.ParsedStorageEntry
	if err := json.Unmarshal(file.ParseResult, &parsed); err != nil {
		q.UpdateLibraryFileStatus(ctx, sqlc.UpdateLibraryFileStatusParams{
			ID:           file.ID,
			Status:       sqlc.FileStatusError,
			ErrorMessage: "unparseable result",
		})
		return nil
	}

	mediaType := sqlc.MediaType(job.Args.MediaType)
	err = w.Matcher.MatchSingleFile(ctx, file, mediaType, job.Args.LibraryID)
	if err != nil {
		log.Warn().Err(err).Int64("file_id", file.ID).Msg("match error")
		return nil
	}

	updated, err := q.GetLibraryFileByID(ctx, file.ID)
	if err != nil {
		return nil
	}

	if updated.Status == sqlc.FileStatusMatched && updated.MediaItemID.Valid {
		client := river.ClientFromContext[pgx.Tx](ctx)

		client.Insert(ctx, DetectLocalAssetsArgs{
			MediaItemID:   updated.MediaItemID.Int64,
			LibraryFileID: file.ID,
			FilePath:      file.Path,
			MediaType:     job.Args.MediaType,
		}, nil)

		w.enqueueRemoteImages(ctx, client, updated.MediaItemID.Int64, job.Args.MediaType)
	}

	return nil
}

func (w *MetadataMatchWorker) enqueueRemoteImages(ctx context.Context, client *river.Client[pgx.Tx], mediaItemID int64, mediaType string) {
	q := sqlc.New(w.DB)
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return
	}

	if item.PosterPath != "" && isURL(item.PosterPath) {
		client.Insert(ctx, DownloadImageArgs{
			MediaItemID: mediaItemID,
			URL:         item.PosterPath,
			AssetType:   "poster",
			MediaType:   mediaType,
			SortOrder:   0,
		}, nil)
	}

	if item.BackdropPath != "" && isURL(item.BackdropPath) {
		client.Insert(ctx, DownloadImageArgs{
			MediaItemID: mediaItemID,
			URL:         item.BackdropPath,
			AssetType:   "backdrop",
			MediaType:   mediaType,
			SortOrder:   0,
		}, nil)
	}
}

func isURL(s string) bool {
	return len(s) > 8 && (s[:7] == "http://" || s[:8] == "https://")
}

func init() {
	_ = fmt.Sprintf
}
