package scheduler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/metadata/heyamedia"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type RefreshMetadataTask struct {
	DB    *pgxpool.Pool
	River *river.Client[pgx.Tx]
}

func (t *RefreshMetadataTask) ID() TaskID { return TaskRefreshMetadata }

type staleItem struct {
	MediaItemID   int64
	LibraryFileID int64
	FilePath      string
	MediaType     string
	ExternalIDs   []byte
	HeyaSlug      string
	Title         string
}

func (t *RefreshMetadataTask) findStaleItems(ctx context.Context) ([]staleItem, error) {
	rows, err := t.DB.Query(ctx, `
		SELECT mi.id, lf.id, lf.path, mi.media_type, mi.external_ids, mi.heya_slug, mi.title,
		       l.settings
		FROM media_items mi
		JOIN library_files lf ON lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.external_ids != '{}'
		ORDER BY mi.metadata_refreshed_at ASC NULLS FIRST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []staleItem
	for rows.Next() {
		var item staleItem
		var settingsJSON []byte
		if err := rows.Scan(&item.MediaItemID, &item.LibraryFileID, &item.FilePath, &item.MediaType, &item.ExternalIDs, &item.HeyaSlug, &item.Title, &settingsJSON); err != nil {
			continue
		}

		settings := metadata.ParseSettings(settingsJSON)
		if settings.MetadataRefreshDays <= 0 {
			continue
		}

		items = append(items, item)
	}
	return items, rows.Err()
}

func (t *RefreshMetadataTask) findStaleItemsFiltered(ctx context.Context) ([]staleItem, error) {
	rows, err := t.DB.Query(ctx, `
		SELECT mi.id, lf.id, lf.path, mi.media_type, mi.external_ids, mi.heya_slug, mi.title,
		       l.settings, mi.metadata_refreshed_at
		FROM media_items mi
		JOIN library_files lf ON lf.media_item_id = mi.id AND lf.deleted_at IS NULL
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.external_ids != '{}'
		ORDER BY mi.metadata_refreshed_at ASC NULLS FIRST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var items []staleItem
	for rows.Next() {
		var item staleItem
		var settingsJSON []byte
		var refreshedAt *time.Time
		if err := rows.Scan(&item.MediaItemID, &item.LibraryFileID, &item.FilePath, &item.MediaType, &item.ExternalIDs, &item.HeyaSlug, &item.Title, &settingsJSON, &refreshedAt); err != nil {
			continue
		}

		settings := metadata.ParseSettings(settingsJSON)
		if settings.MetadataRefreshDays <= 0 {
			continue
		}

		if refreshedAt == nil {
			continue
		}

		cutoff := now.AddDate(0, 0, -settings.MetadataRefreshDays)
		if refreshedAt.After(cutoff) {
			continue
		}

		items = append(items, item)
	}
	return items, rows.Err()
}

func (t *RefreshMetadataTask) CountPending(ctx context.Context) (int, error) {
	items, err := t.findStaleItemsFiltered(ctx)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (t *RefreshMetadataTask) Run(ctx context.Context, progress *ProgressTracker) error {
	items, err := t.findStaleItemsFiltered(ctx)
	if err != nil {
		return err
	}

	progress.SetTotal(len(items))

	for _, item := range items {
		if ctx.Err() != nil {
			return nil
		}

		providerName, providerID := pickRefreshProvider(item.HeyaSlug, item.ExternalIDs)
		if providerName == "" {
			progress.Fail(item.Title)
			continue
		}

		t.River.Insert(ctx, worker.MetadataFetchArgs{
			MediaItemID:   item.MediaItemID,
			LibraryID:     0,
			LibraryFileID: item.LibraryFileID,
			FilePath:      item.FilePath,
			MediaType:     item.MediaType,
			ProviderName:  providerName,
			ProviderID:    providerID,
		}, nil)

		progress.Advance(item.Title)
	}

	if progress.Snapshot().Completed > 0 {
		log.Info().Int("enqueued", progress.Snapshot().Completed).Msg("refresh metadata: items enqueued")
	}

	return nil
}

func pickRefreshProvider(heyaSlug string, externalIDsJSON []byte) (string, string) {
	var ids map[string]string
	json.Unmarshal(externalIDsJSON, &ids)

	if pid := heyamedia.BuildLookupID(heyaSlug, ids); pid != "" {
		return "heya", pid
	}
	return "", ""
}
