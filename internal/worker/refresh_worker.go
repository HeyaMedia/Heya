package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type MetadataRefreshWorker struct {
	river.WorkerDefaults[MetadataRefreshArgs]
	DB *pgxpool.Pool
}

func (w *MetadataRefreshWorker) Work(ctx context.Context, job *river.Job[MetadataRefreshArgs]) error {
	q := sqlc.New(w.DB)

	lib, err := q.GetLibraryByID(ctx, job.Args.LibraryID)
	if err != nil {
		return nil
	}

	settings := metadata.ParseSettings(lib.Settings)
	if settings.MetadataRefreshDays <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -settings.MetadataRefreshDays)

	rows, err := w.DB.Query(ctx,
		`SELECT mi.id, lf.id AS file_id, lf.path,
		        mi.media_type, mi.external_ids
		 FROM media_items mi
		 JOIN library_files lf ON lf.media_item_id = mi.id
		 WHERE mi.library_id = $1
		   AND (mi.metadata_refreshed_at IS NULL OR mi.metadata_refreshed_at < $2)
		 LIMIT 50`,
		job.Args.LibraryID, cutoff,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	client := river.ClientFromContext[pgx.Tx](ctx)
	enqueued := 0

	for rows.Next() {
		var mediaItemID, fileID int64
		var filePath, mediaType string
		var externalIDsJSON []byte

		if err := rows.Scan(&mediaItemID, &fileID, &filePath, &mediaType, &externalIDsJSON); err != nil {
			continue
		}

		providerName, providerID := pickRefreshProvider(mediaType, externalIDsJSON)
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

	if enqueued > 0 {
		log.Info().Int64("library_id", job.Args.LibraryID).Int("enqueued", enqueued).Msg("metadata refresh enqueued")
	}

	return nil
}

func pickRefreshProvider(mediaType string, externalIDsJSON []byte) (string, string) {
	var ids map[string]string
	json.Unmarshal(externalIDsJSON, &ids)

	if tmdbID := ids["tmdb"]; tmdbID != "" {
		prefix := "movie"
		if mediaType == "tv" {
			prefix = "tv"
		}
		return "tmdb", prefix + ":" + tmdbID
	}

	if tvdbID := ids["tvdb"]; tvdbID != "" && mediaType == "tv" {
		return "tvdb", "tvdb:series:" + tvdbID
	}

	if mbid := ids["musicbrainz"]; mbid != "" {
		return "musicbrainz", "musicbrainz:" + mbid
	}

	if olid := ids["openlibrary"]; olid != "" {
		return "openlibrary", "openlibrary:" + olid
	}

	return "", ""
}
