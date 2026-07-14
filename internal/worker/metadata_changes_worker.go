package worker

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const (
	metadataChangeConsumer = "heya-read-models"
	metadataChangePageSize = int64(500)
)

type metadataChangeSource interface {
	Changes(context.Context, int64, int64) (heyametadata.ChangePage, error)
}

// SyncMetadataChangesWorker consumes HeyaMetadata's gap-free sequence and
// transactionally turns each page into durable local refresh jobs. Advancing
// the cursor and inserting River jobs in the same pgx transaction means a
// crash can produce neither a lost change nor an unbounded duplicate fanout.
type SyncMetadataChangesWorker struct {
	river.WorkerDefaults[SyncMetadataChangesArgs]
	DB     *pgxpool.Pool
	Source metadataChangeSource
}

func (w *SyncMetadataChangesWorker) Work(ctx context.Context, _ *river.Job[SyncMetadataChangesArgs]) error {
	if w.Source == nil {
		return fmt.Errorf("sync metadata changes: metadata client is required")
	}
	rc := river.ClientFromContext[pgx.Tx](ctx)
	if rc == nil {
		return fmt.Errorf("sync metadata changes: river client unavailable")
	}

	q := sqlc.New(w.DB)
	cursor, err := q.GetMetadataChangeCursor(ctx, metadataChangeConsumer)
	if err != nil {
		return fmt.Errorf("read metadata change cursor: %w", err)
	}
	seenMedia := make(map[int64]struct{})
	seenPeople := make(map[int64]struct{})
	pages, changes, enqueued := 0, 0, 0
	backfillChecked := false

	for {
		page, err := w.Source.Changes(ctx, cursor, metadataChangePageSize)
		if err != nil {
			return err
		}
		if page.NextCursor < cursor {
			return fmt.Errorf("metadata changes cursor regressed from %d to %d", cursor, page.NextCursor)
		}

		tx, err := w.DB.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return fmt.Errorf("begin metadata change page: %w", err)
		}
		pageErr := func() error {
			defer func() { _ = tx.Rollback(ctx) }()
			qtx := sqlc.New(tx)
			for _, change := range page.Entries {
				entityID, parseErr := uuid.Parse(change.EntityID)
				if parseErr != nil {
					return fmt.Errorf("change %d has invalid entity ID %q: %w", change.Sequence, change.EntityID, parseErr)
				}
				bindings, listErr := qtx.ListMetadataBindingsByEntity(ctx, entityID)
				if listErr != nil {
					return fmt.Errorf("list bindings for metadata change %d: %w", change.Sequence, listErr)
				}
				for _, binding := range bindings {
					if change.ProjectionVersion > 0 && binding.ProjectionVersion >= change.ProjectionVersion && change.ChangeType != "redirected" {
						continue
					}
					if binding.LocalKind == "person" {
						if _, exists := seenPeople[binding.LocalID]; exists {
							continue
						}
						seenPeople[binding.LocalID] = struct{}{}
						args := PersonFetchArgs{PersonID: binding.LocalID, EntityID: change.EntityID, Force: true}
						opts := args.InsertOpts()
						if _, insertErr := rc.InsertTx(ctx, tx, args, &opts); insertErr != nil {
							return fmt.Errorf("enqueue person %d for metadata change: %w", binding.LocalID, insertErr)
						}
						enqueued++
						continue
					}
					mediaIDs, resolveErr := localMediaItemIDs(ctx, tx, binding.LocalKind, binding.LocalID)
					if resolveErr != nil {
						return fmt.Errorf("resolve %s %d for metadata change: %w", binding.LocalKind, binding.LocalID, resolveErr)
					}
					for _, mediaID := range mediaIDs {
						if _, exists := seenMedia[mediaID]; exists {
							continue
						}
						seenMedia[mediaID] = struct{}{}
						args := EnrichMediaItemArgs{ItemID: mediaID, Source: "metadata_change", Force: true}
						opts := args.InsertOpts()
						if _, insertErr := rc.InsertTx(ctx, tx, args, &opts); insertErr != nil {
							return fmt.Errorf("enqueue media item %d for metadata change: %w", mediaID, insertErr)
						}
						enqueued++
					}
				}
			}
			if !backfillChecked {
				queued, backfillErr := enqueueOneMetadataBindingBackfill(ctx, tx, rc)
				if backfillErr != nil {
					return backfillErr
				}
				if queued {
					enqueued++
				}
				backfillChecked = true
			}
			if err := qtx.CommitMetadataChangeCursor(ctx, sqlc.CommitMetadataChangeCursorParams{
				Consumer: metadataChangeConsumer, NextCursor: page.NextCursor,
			}); err != nil {
				return fmt.Errorf("commit metadata change cursor: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("commit metadata change page: %w", err)
			}
			return nil
		}()
		if pageErr != nil {
			return pageErr
		}

		pages++
		changes += len(page.Entries)
		if len(page.Entries) < int(metadataChangePageSize) || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}

	if changes > 0 {
		log.Info().Int("pages", pages).Int("changes", changes).Int("enqueued", enqueued).Msg("heyametadata change feed synchronized")
	}
	return nil
}

// enqueueOneMetadataBindingBackfill steadily upgrades pre-V2 libraries
// without recreating the retired blind staleness sweep. Absence of a binding
// is the durable work marker; active River rows are excluded so each 30-second
// tick advances to another item instead of piling duplicates onto a slow one.
func enqueueOneMetadataBindingBackfill(ctx context.Context, tx pgx.Tx, rc *river.Client[pgx.Tx]) (bool, error) {
	var mediaID int64
	err := tx.QueryRow(ctx, `
		SELECT media.id
		FROM media_item_cards media
		LEFT JOIN metadata_entity_bindings binding
		  ON binding.local_kind = 'media_item' AND binding.local_id = media.id
		WHERE binding.local_id IS NULL
		  AND (media.heya_slug <> '' OR media.external_ids <> '{}'::jsonb)
		  AND NOT EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.kind = 'enrich_media_item'
		      AND job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
		      AND NULLIF(job.args->>'item_id', '')::bigint = media.id
		  )
		ORDER BY media.id
		LIMIT 1`).Scan(&mediaID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("select metadata binding backfill: %w", err)
	}
	args := EnrichMediaItemArgs{ItemID: mediaID, Source: "metadata_binding_backfill", Force: true}
	opts := args.InsertOpts()
	opts.Priority = PriorityAnalysis
	opts.UniqueOpts = uniqueWhileActive()
	if _, err := rc.InsertTx(ctx, tx, args, &opts); err != nil {
		return false, fmt.Errorf("enqueue metadata binding backfill for %d: %w", mediaID, err)
	}
	return true, nil
}

func localMediaItemIDs(ctx context.Context, tx pgx.Tx, localKind string, localID int64) ([]int64, error) {
	query := ""
	switch localKind {
	case "media_item":
		return []int64{localID}, nil
	case "artist":
		query = `SELECT media_item_id FROM artists WHERE id = $1`
	case "album":
		query = `SELECT artist.media_item_id FROM albums album JOIN artists artist ON artist.id = album.artist_id WHERE album.id = $1`
	case "track":
		query = `SELECT artist.media_item_id FROM tracks track JOIN albums album ON album.id = track.album_id JOIN artists artist ON artist.id = album.artist_id WHERE track.id = $1`
	case "tv_season":
		query = `SELECT series.media_item_id FROM tv_seasons season JOIN tv_series series ON series.id = season.series_id WHERE season.id = $1`
	case "tv_episode":
		query = `SELECT series.media_item_id FROM tv_episodes episode JOIN tv_seasons season ON season.id = episode.season_id JOIN tv_series series ON series.id = season.series_id WHERE episode.id = $1`
	case "author":
		rows, err := tx.Query(ctx, `SELECT media_item_id FROM books WHERE author_id = $1 ORDER BY media_item_id`, localID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var result []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			result = append(result, id)
		}
		return result, rows.Err()
	default:
		return nil, nil
	}
	var mediaID int64
	if err := tx.QueryRow(ctx, query, localID).Scan(&mediaID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return []int64{mediaID}, nil
}
