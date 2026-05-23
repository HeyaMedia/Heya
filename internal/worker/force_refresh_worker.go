package worker

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
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
	q := sqlc.New(w.DB)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return nil
	}

	client := river.ClientFromContext[pgx.Tx](ctx)

	// Music libraries refresh per-artist (RefreshMusicArtistWorker handles
	// album + track enrichment off one heya.media call). The per-file
	// MetadataFetch path used for movies/TV would call the legacy
	// createMusic and either duplicate or trip our dedupe constraints.
	if lib.MediaType == sqlc.MediaTypeMusic {
		artists, err := q.ListArtistsByLibrary(ctx, job.Args.LibraryID)
		if err != nil {
			return nil
		}
		enqueued := 0
		for i, a := range artists {
			if _, err := client.Insert(ctx, RefreshMusicArtistArgs{
				ArtistID:       a.ID,
				Force:          true,
				BatchLibraryID: lib.ID,
				BatchTotal:     len(artists),
				BatchPosition:  i + 1,
			}, nil); err != nil {
				log.Warn().Err(err).Int64("artist_id", a.ID).Msg("enqueue RefreshMusicArtist failed")
				continue
			}
			enqueued++
		}
		log.Info().Int64("library_id", job.Args.LibraryID).Int("artists_enqueued", enqueued).Msg("force music refresh enqueued")
		return nil
	}

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
	q := sqlc.New(w.DB)
	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return nil
	}

	client := river.ClientFromContext[pgx.Tx](ctx)

	// Music: clear cover paths and route through RefreshMusicArtist (same
	// enrichment cycle covers metadata + artwork from heya.media).
	if lib.MediaType == sqlc.MediaTypeMusic {
		_, _ = w.DB.Exec(ctx,
			`UPDATE albums SET cover_path = '' WHERE artist_id IN
			   (SELECT a.id FROM artists a
			    JOIN media_items mi ON mi.id = a.media_item_id
			    WHERE mi.library_id = $1)`,
			job.Args.LibraryID,
		)
		artists, err := q.ListArtistsByLibrary(ctx, job.Args.LibraryID)
		if err != nil {
			return nil
		}
		enqueued := 0
		for i, a := range artists {
			if _, err := client.Insert(ctx, RefreshMusicArtistArgs{
				ArtistID:       a.ID,
				Force:          true,
				BatchLibraryID: lib.ID,
				BatchTotal:     len(artists),
				BatchPosition:  i + 1,
			}, nil); err != nil {
				continue
			}
			enqueued++
		}
		log.Info().Int64("library_id", job.Args.LibraryID).Int("artists_enqueued", enqueued).Msg("force music image refresh enqueued")
		return nil
	}

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
