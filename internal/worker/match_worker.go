package worker

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type MetadataMatchWorker struct {
	river.WorkerDefaults[MetadataMatchArgs]
	DB      *pgxpool.Pool
	Matcher MatchService
	Heya    *heyamedia.HeyaProvider
	Hub     EventPublisher
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
	matchResult, err := w.Matcher.MatchSingleFile(ctx, file, mediaType, job.Args.LibraryID)
	if err != nil {
		log.Warn().Err(err).Int64("file_id", file.ID).Msg("match error")
		return nil
	}

	updated, err := q.GetLibraryFileByID(ctx, file.ID)
	if err != nil {
		return nil
	}

	if updated.Status == sqlc.FileStatusMatched && updated.MediaItemID.Valid {
		if w.Hub != nil {
			w.Hub.Emit(eventhub.EventMediaAdded, eventhub.MediaPayload{
				MediaItemID: updated.MediaItemID.Int64,
				LibraryID:   job.Args.LibraryID,
				MediaType:   job.Args.MediaType,
			})
		}

		// Stub match is on disk; queue the unified enrich job to fill in
		// the rest. Only on first match — re-matches preserve existing
		// enriched state.
		if matchResult.IsNew {
			if err := EnqueueEnrichTx(ctx, updated.MediaItemID.Int64, mediaType, EnrichSourceScan); err != nil {
				log.Warn().Err(err).Int64("media_id", updated.MediaItemID.Int64).Msg("enqueue enrich after match failed")
			}
		}

		log.Info().
			Int64("media_id", updated.MediaItemID.Int64).
			Bool("new", matchResult.IsNew).
			Msg("matched")
	}

	return nil
}
