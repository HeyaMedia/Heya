package worker

import (
	"context"
	"encoding/json"

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

		client := river.ClientFromContext[pgx.Tx](ctx)

		// Music: enrichment happens out-of-band via RefreshMusicArtistWorker
		// (heya.media has no per-album fetch yet — the artist payload carries
		// the full discography). Fan that out instead of MetadataFetchArgs.
		if mediaType == sqlc.MediaTypeMusic {
			if matchResult.IsNew && matchResult.ArtistID > 0 {
				_, _ = client.Insert(ctx, RefreshMusicArtistArgs{ArtistID: matchResult.ArtistID}, nil)
			}
		} else if matchResult.IsNew {
			client.Insert(ctx, MetadataFetchArgs{
				MediaItemID:   updated.MediaItemID.Int64,
				LibraryID:     job.Args.LibraryID,
				LibraryFileID: file.ID,
				FilePath:      file.Path,
				MediaType:     job.Args.MediaType,
				ProviderName:  matchResult.ProviderName,
				ProviderID:    matchResult.ProviderID,
			}, nil)
		}

		log.Info().
			Int64("media_id", updated.MediaItemID.Int64).
			Bool("new", matchResult.IsNew).
			Msg("matched")
	}

	return nil
}
