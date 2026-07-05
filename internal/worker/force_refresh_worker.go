package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

func pickRefreshProvider(mediaType string, externalIDsJSON []byte, heyaSlug string) (string, string) {
	var ids map[string]string
	json.Unmarshal(externalIDsJSON, &ids)

	if pid := heyamedia.BuildLookupID(metadata.MediaKind(mediaType), ids, heyaSlug); pid != "" {
		return "heya", pid
	}
	return "", ""
}

type ForceRefreshMetadataWorker struct {
	river.WorkerDefaults[ForceRefreshMetadataArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ForceRefreshMetadataWorker) Work(ctx context.Context, job *river.Job[ForceRefreshMetadataArgs]) error {
	start := time.Now()
	q := sqlc.New(w.DB)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		log.Debug().Err(err).Int64("library_id", job.Args.LibraryID).Msg("force_refresh_metadata: library not found, skipping")
		return nil
	}
	w.Progress.SetCurrentByKind(ForceRefreshMetadataArgs{}.Kind(), lib.Name)
	log.Debug().Int64("library_id", job.Args.LibraryID).Str("library", lib.Name).Msg("force_refresh_metadata: job started")

	enqueued, err := enqueueForceForLibrary(ctx, w.DB, job.Args.LibraryID)
	if err != nil {
		log.Warn().Err(err).Int64("library_id", job.Args.LibraryID).Msg("force_refresh_metadata: enumerate failed")
	}
	log.Debug().Int64("library_id", job.Args.LibraryID).Dur("duration", time.Since(start)).Msg("force_refresh_metadata: job finished")
	log.Info().Int64("library_id", job.Args.LibraryID).Int("enqueued", enqueued).Msg("force metadata refresh enqueued")
	return nil
}

type ForceRefreshImagesWorker struct {
	river.WorkerDefaults[ForceRefreshImagesArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *ForceRefreshImagesWorker) Work(ctx context.Context, job *river.Job[ForceRefreshImagesArgs]) error {
	start := time.Now()
	q := sqlc.New(w.DB)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		log.Debug().Err(err).Int64("library_id", job.Args.LibraryID).Msg("force_refresh_images: library not found, skipping")
		return nil
	}
	w.Progress.SetCurrentByKind(ForceRefreshImagesArgs{}.Kind(), lib.Name)
	log.Debug().Int64("library_id", job.Args.LibraryID).Str("library", lib.Name).Str("media_type", string(lib.MediaType)).Msg("force_refresh_images: job started")

	// Wipe the existing remote-sourced assets so the enrich pass actually
	// re-downloads. Music albums use cover_path on the album row; other
	// media types track per-asset rows in media_assets.
	if lib.MediaType == sqlc.MediaTypeMusic {
		tag, _ := w.DB.Exec(ctx,
			`UPDATE albums SET cover_path = '' WHERE artist_id IN
			   (SELECT a.id FROM artists a
			    JOIN media_items mi ON mi.id = a.media_item_id
			    WHERE mi.library_id = $1)`,
			job.Args.LibraryID,
		)
		log.Debug().Int64("library_id", job.Args.LibraryID).Int64("albums_cleared", tag.RowsAffected()).Msg("force_refresh_images: cleared album covers")
	} else {
		libraryItemIDs := `SELECT id FROM media_items WHERE library_id = $1`
		tag, _ := w.DB.Exec(ctx,
			`DELETE FROM media_assets
			 WHERE media_item_id IN (`+libraryItemIDs+`)
			   AND (source = 'remote' OR label ~ '^(season|s\d+e\d+)')`,
			job.Args.LibraryID,
		)
		log.Debug().Int64("library_id", job.Args.LibraryID).Int64("assets_deleted", tag.RowsAffected()).Msg("force_refresh_images: cleared remote assets")
		tag2, _ := w.DB.Exec(ctx,
			`UPDATE media_items SET poster_path = '', backdrop_path = ''
			 WHERE library_id = $1`,
			job.Args.LibraryID,
		)
		log.Debug().Int64("library_id", job.Args.LibraryID).Int64("items_cleared", tag2.RowsAffected()).Msg("force_refresh_images: cleared poster/backdrop paths")
	}

	enqueued, err := enqueueForceForLibrary(ctx, w.DB, job.Args.LibraryID)
	if err != nil {
		log.Warn().Err(err).Int64("library_id", job.Args.LibraryID).Msg("force_refresh_images: enumerate failed")
	}
	log.Debug().Int64("library_id", job.Args.LibraryID).Dur("duration", time.Since(start)).Msg("force_refresh_images: job finished")
	log.Info().Int64("library_id", job.Args.LibraryID).Int("enqueued", enqueued).Msg("force image refresh enqueued")
	return nil
}

// enqueueForceForLibrary fans out a forced enrich for every media item in
// the library. Single pass — the unified enrich worker handles type-specific
// branching internally, so we don't need separate music / non-music paths
// anymore.
func enqueueForceForLibrary(ctx context.Context, db *pgxpool.Pool, libraryID int64) (int, error) {
	rows, err := db.Query(ctx,
		`SELECT mi.id, mi.media_type, mi.external_ids, mi.heya_slug
		 FROM media_items mi
		 WHERE mi.library_id = $1`,
		libraryID,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	enqueued := 0
	skippedNoProvider := 0
	skippedScanErr := 0
	for rows.Next() {
		var itemID int64
		var mediaType string
		var externalIDsJSON []byte
		var heyaSlug string
		if err := rows.Scan(&itemID, &mediaType, &externalIDsJSON, &heyaSlug); err != nil {
			skippedScanErr++
			continue
		}

		// Skip items the enrich worker would just fail on. The matcher
		// stamps external_ids during the search stub, so this should only
		// catch genuinely unmatched stubs.
		if _, providerID := pickRefreshProvider(mediaType, externalIDsJSON, heyaSlug); providerID == "" && sqlc.MediaType(mediaType) != sqlc.MediaTypeMusic {
			skippedNoProvider++
			continue
		}

		if err := EnqueueEnrichTx(ctx, itemID, sqlc.MediaType(mediaType), EnrichSourceForced); err != nil {
			log.Warn().Err(err).Int64("item_id", itemID).Msg("enqueue forced enrich failed")
			continue
		}
		enqueued++
	}
	log.Debug().Int64("library_id", libraryID).Int("enqueued", enqueued).Int("skipped_no_provider_id", skippedNoProvider).Int("skipped_scan_error", skippedScanErr).Msg("force_refresh: item enumeration done")
	return enqueued, rows.Err()
}
