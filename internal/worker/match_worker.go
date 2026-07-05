package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
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
	DB       *pgxpool.Pool
	Matcher  MatchService
	Heya     *heyamedia.HeyaProvider
	Hub      EventPublisher
	Progress *TaskProgressBroadcaster
}

func (w *MetadataMatchWorker) Work(ctx context.Context, job *river.Job[MetadataMatchArgs]) error {
	start := time.Now()
	q := sqlc.New(w.DB)

	file, err := q.GetLibraryFileByID(ctx, job.Args.LibraryFileID)
	if err != nil {
		log.Debug().Err(err).Int64("file_id", job.Args.LibraryFileID).Msg("match: library file lookup failed, skipping")
		return err
	}

	if file.Status == sqlc.FileStatusMatched {
		log.Debug().Int64("file_id", file.ID).Msg("match: file already matched, skipping")
		return nil
	}

	w.Progress.SetCurrent(MetadataMatchArgs{}.Kind(), job.Args.ScheduledTaskID, filepath.Base(file.Path))

	log.Debug().Int64("file_id", file.ID).Int64("library_id", job.Args.LibraryID).Str("media_type", job.Args.MediaType).Str("path", filepath.Base(file.Path)).Msg("match: job started")

	var parsed parser.ParsedStorageEntry
	if err := json.Unmarshal(file.ParseResult, &parsed); err != nil {
		log.Debug().Err(err).Int64("file_id", file.ID).Msg("match: unparseable parse result, marking file error")
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
	log.Debug().Int64("file_id", file.ID).Str("provider", matchResult.ProviderName).Str("provider_id", matchResult.ProviderID).Bool("new", matchResult.IsNew).Msg("match: matcher returned")

	updated, err := q.GetLibraryFileByID(ctx, file.ID)
	if err != nil {
		log.Debug().Err(err).Int64("file_id", file.ID).Msg("match: re-fetch after match failed, skipping")
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
			client := river.ClientFromContext[pgx.Tx](ctx)
			if client == nil {
				log.Warn().Int64("media_id", updated.MediaItemID.Int64).Msg("enqueue enrich after match failed: no river client")
			} else if err := enqueueEnrich(ctx, client, updated.MediaItemID.Int64, mediaType, EnrichSourceScan, false, job.Args.ScheduledTaskID, 0, 0, 0); err != nil {
				log.Warn().Err(err).Int64("media_id", updated.MediaItemID.Int64).Msg("enqueue enrich after match failed")
			} else {
				log.Debug().Int64("media_id", updated.MediaItemID.Int64).Msg("match: enrich enqueued")
			}
		} else {
			log.Debug().Int64("media_id", updated.MediaItemID.Int64).Msg("match: re-match on existing item, skipping enrich enqueue")
		}

		log.Debug().Int64("media_id", updated.MediaItemID.Int64).Dur("duration", time.Since(start)).Msg("match: job finished")
		log.Info().
			Int64("media_id", updated.MediaItemID.Int64).
			Bool("new", matchResult.IsNew).
			Msg("matched")
	} else {
		log.Debug().Int64("file_id", updated.ID).Str("status", string(updated.Status)).Msg("match: file not matched, skipping downstream enqueue")
	}

	return nil
}
