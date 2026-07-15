package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	Changes(context.Context, int64, int64, string) (heyametadata.ChangePage, error)
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
	consumer, err := q.GetMetadataChangeCursor(ctx, metadataChangeConsumer)
	if err != nil {
		return fmt.Errorf("read metadata change cursor: %w", err)
	}
	cursor := consumer.NextCursor
	streamID := metadataChangeStreamString(consumer.StreamID)
	if streamID == "" && cursor != 0 {
		log.Warn().Int64("old_cursor", cursor).Msg("heyametadata legacy cursor has no stream identity; replaying from zero")
		cursor = 0
	}
	seenMedia := make(map[int64]struct{})
	seenPeople := make(map[int64]struct{})
	pages, changes, enqueued := 0, 0, 0
	backfillChecked := false

	for {
		page, err := w.Source.Changes(ctx, cursor, metadataChangePageSize, streamID)
		if err != nil {
			var conflict *heyametadata.ChangeStreamConflict
			if errors.As(err, &conflict) {
				stream, parseErr := metadataChangeStreamUUID(conflict.StreamID)
				if parseErr != nil {
					return fmt.Errorf("reset metadata change cursor: %w", parseErr)
				}
				if resetErr := q.ResetMetadataChangeCursor(ctx, sqlc.ResetMetadataChangeCursorParams{
					Consumer: metadataChangeConsumer, StreamID: stream,
				}); resetErr != nil {
					return fmt.Errorf("reset metadata change cursor: %w", resetErr)
				}
				log.Warn().
					Str("reason", conflict.Code).
					Str("stream_id", conflict.StreamID).
					Int64("old_cursor", cursor).
					Int64("head_cursor", conflict.HeadCursor).
					Msg("heyametadata change stream reset; replaying from zero")
				cursor, streamID = 0, conflict.StreamID
				continue
			}
			return err
		}
		if page.StreamID == "" {
			return fmt.Errorf("metadata changes response has no stream ID")
		}
		pageStream, err := metadataChangeStreamUUID(page.StreamID)
		if err != nil {
			return err
		}
		if page.NextCursor > page.HeadCursor {
			return fmt.Errorf("metadata changes cursor %d exceeds reported head %d", page.NextCursor, page.HeadCursor)
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
			type pageChange struct {
				change heyametadata.Change
				force  bool
			}
			changesByEntity := make(map[uuid.UUID]pageChange, len(page.Entries))
			entityIDs := make([]uuid.UUID, 0, len(page.Entries))
			for _, change := range page.Entries {
				entityID, parseErr := uuid.Parse(change.EntityID)
				if parseErr != nil {
					return fmt.Errorf("change %d has invalid entity ID %q: %w", change.Sequence, change.EntityID, parseErr)
				}
				state, exists := changesByEntity[entityID]
				if !exists {
					entityIDs = append(entityIDs, entityID)
				}
				state.change = change
				state.force = state.force || change.ChangeType == "redirected"
				changesByEntity[entityID] = state
			}

			targets, listErr := qtx.ListMetadataChangeTargetsByEntities(ctx, entityIDs)
			if listErr != nil {
				return fmt.Errorf("resolve metadata change page targets: %w", listErr)
			}
			jobs := make([]river.InsertManyParams, 0, len(targets))
			for _, target := range targets {
				state := changesByEntity[target.EntityID]
				change := state.change
				if change.ProjectionVersion > 0 && target.ProjectionVersion >= change.ProjectionVersion && !state.force {
					continue
				}
				switch target.TargetKind {
				case "person":
					if _, exists := seenPeople[target.TargetID]; exists {
						continue
					}
					seenPeople[target.TargetID] = struct{}{}
					args := PersonFetchArgs{PersonID: target.TargetID, EntityID: target.EntityID.String(), Force: true}
					opts := args.InsertOpts()
					jobs = append(jobs, river.InsertManyParams{Args: args, InsertOpts: &opts})
				case "media_item":
					if _, exists := seenMedia[target.TargetID]; exists {
						continue
					}
					seenMedia[target.TargetID] = struct{}{}
					args := EnrichMediaItemArgs{ItemID: target.TargetID, Source: "metadata_change", Force: true}
					opts := args.InsertOpts()
					jobs = append(jobs, river.InsertManyParams{Args: args, InsertOpts: &opts})
				}
			}
			if len(jobs) > 0 {
				results, insertErr := rc.InsertManyTx(ctx, tx, jobs)
				if insertErr != nil {
					return fmt.Errorf("enqueue metadata change page: %w", insertErr)
				}
				for _, result := range results {
					if !result.UniqueSkippedAsDuplicate {
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
				StreamID: pageStream,
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
		streamID = page.StreamID
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

func metadataChangeStreamString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

func metadataChangeStreamUUID(value string) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid metadata change stream ID %q: %w", value, err)
	}
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}, nil
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
