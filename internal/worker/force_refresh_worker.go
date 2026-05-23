package worker

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

func pickRefreshProvider(heyaSlug string, externalIDsJSON []byte) (string, string) {
	var ids map[string]string
	json.Unmarshal(externalIDsJSON, &ids)

	if pid := heyamedia.BuildLookupID(heyaSlug, ids); pid != "" {
		return "heya", pid
	}
	return "", ""
}

type ForceRefreshMetadataWorker struct {
	river.WorkerDefaults[ForceRefreshMetadataArgs]
	DB *pgxpool.Pool
}

func (w *ForceRefreshMetadataWorker) Work(ctx context.Context, job *river.Job[ForceRefreshMetadataArgs]) error {
	rows, err := w.DB.Query(ctx,
		`SELECT mi.id, lf.id AS file_id, lf.path,
		        mi.media_type, mi.external_ids, mi.heya_slug
		 FROM media_items mi
		 JOIN library_files lf ON lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		 WHERE mi.library_id = $1`,
		job.Args.LibraryID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	client := river.ClientFromContext[pgx.Tx](ctx)
	enqueued := 0

	for rows.Next() {
		var mediaItemID, fileID int64
		var filePath, mediaType, heyaSlug string
		var externalIDsJSON []byte

		if err := rows.Scan(&mediaItemID, &fileID, &filePath, &mediaType, &externalIDsJSON, &heyaSlug); err != nil {
			continue
		}

		providerName, providerID := pickRefreshProvider(heyaSlug, externalIDsJSON)
		if providerName == "" {
			continue
		}

		client.Insert(ctx, MetadataFetchArgs{
			MediaItemID:   mediaItemID,
			LibraryID:     job.Args.LibraryID,
			LibraryFileID: fileID,
			FilePath:      filePath,
			MediaType:     mediaType,
			ProviderName:  providerName,
			ProviderID:    providerID,
		}, nil)
		enqueued++
	}

	log.Info().Int64("library_id", job.Args.LibraryID).Int("enqueued", enqueued).Msg("force metadata refresh enqueued")
	return nil
}

type ForceRefreshImagesWorker struct {
	river.WorkerDefaults[ForceRefreshImagesArgs]
	DB *pgxpool.Pool
}

func (w *ForceRefreshImagesWorker) Work(ctx context.Context, job *river.Job[ForceRefreshImagesArgs]) error {
	libraryItemIDs := `SELECT id FROM media_items WHERE library_id = $1`

	// Clear remote-sourced assets and any with season/episode labels
	w.DB.Exec(ctx,
		`DELETE FROM media_assets
		 WHERE media_item_id IN (`+libraryItemIDs+`)
		   AND (source = 'remote' OR label ~ '^(season|s\d+e\d+)')`,
		job.Args.LibraryID,
	)

	// Reset poster/backdrop paths so the download worker doesn't skip them
	w.DB.Exec(ctx,
		`UPDATE media_items SET poster_path = '', backdrop_path = ''
		 WHERE library_id = $1`,
		job.Args.LibraryID,
	)

	rows, err := w.DB.Query(ctx,
		`SELECT mi.id, lf.id AS file_id, lf.path,
		        mi.media_type, mi.external_ids, mi.heya_slug
		 FROM media_items mi
		 JOIN library_files lf ON lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		 WHERE mi.library_id = $1`,
		job.Args.LibraryID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	client := river.ClientFromContext[pgx.Tx](ctx)
	enqueued := 0

	for rows.Next() {
		var mediaItemID, fileID int64
		var filePath, mediaType, heyaSlug string
		var externalIDsJSON []byte

		if err := rows.Scan(&mediaItemID, &fileID, &filePath, &mediaType, &externalIDsJSON, &heyaSlug); err != nil {
			continue
		}

		providerName, providerID := pickRefreshProvider(heyaSlug, externalIDsJSON)
		if providerName == "" {
			continue
		}

		client.Insert(ctx, MetadataFetchArgs{
			MediaItemID:   mediaItemID,
			LibraryID:     job.Args.LibraryID,
			LibraryFileID: fileID,
			FilePath:      filePath,
			MediaType:     mediaType,
			ProviderName:  providerName,
			ProviderID:    providerID,
		}, nil)
		enqueued++
	}

	log.Info().Int64("library_id", job.Args.LibraryID).Int("enqueued", enqueued).Msg("force image refresh enqueued")
	return nil
}
