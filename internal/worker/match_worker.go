package worker

import (
	"context"
	"encoding/json"

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
		matchResult := w.Matcher.LastMatchResult()

		client := river.ClientFromContext[pgx.Tx](ctx)
		client.Insert(ctx, MetadataFetchArgs{
			MediaItemID:   updated.MediaItemID.Int64,
			LibraryID:     job.Args.LibraryID,
			LibraryFileID: file.ID,
			FilePath:      file.Path,
			MediaType:     job.Args.MediaType,
			ProviderName:  matchResult.ProviderName,
			ProviderID:    matchResult.ProviderID,
		}, nil)

		log.Info().
			Int64("media_id", updated.MediaItemID.Int64).
			Str("provider", matchResult.ProviderName).
			Str("provider_id", matchResult.ProviderID).
			Msg("matched, enqueued metadata fetch")
	}

	return nil
}
