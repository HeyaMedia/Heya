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
	"github.com/karbowiak/heya/internal/metadatasync"
	"github.com/karbowiak/heya/internal/queueops"
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
	// Older builds allowed every feed tick to enqueue another forced refresh
	// for the same parent while HeyaMetadata was still publishing child changes.
	// Repair that backlog on every tick (including an otherwise-empty feed) so
	// deployment immediately converges to one queued trailing refresh per item.
	if cancelled, err := queueops.CoalesceMetadataChangeEnrichJobs(ctx, w.DB); err != nil {
		return fmt.Errorf("coalesce metadata change enrich jobs: %w", err)
	} else if cancelled > 0 {
		log.Info().Int64("cancelled", cancelled).Msg("coalesced redundant metadata change enrich jobs")
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
				scopes map[string]struct{}
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
				if !exists || change.ProjectionVersion >= state.change.ProjectionVersion {
					state.change = change
				}
				state.force = state.force || change.ChangeType == "redirected"
				if state.scopes == nil {
					state.scopes = make(map[string]struct{}, len(change.ChangedScopes))
				}
				for _, scope := range change.ChangedScopes {
					state.scopes[scope] = struct{}{}
				}
				changesByEntity[entityID] = state
			}

			targets, listErr := qtx.ListMetadataChangeTargetsByEntities(ctx, entityIDs)
			if listErr != nil {
				return fmt.Errorf("resolve metadata change page targets: %w", listErr)
			}
			jobs := make([]river.InsertManyParams, 0, len(targets))
			mediaRefreshIDs := make([]int64, 0, len(targets))
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
					mediaRefreshIDs = append(mediaRefreshIDs, target.TargetID)
				}
			}

			// Independently fetched projections cannot use the parent binding's
			// version as proof of success. Route changed_scopes to direct local
			// bindings even when the full-document refresh is already current.
			if len(entityIDs) > 0 {
				scopeTargets, scopeErr := qtx.ListMetadataScopeTargetsByEntities(ctx, entityIDs)
				if scopeErr != nil {
					return fmt.Errorf("resolve metadata scope targets: %w", scopeErr)
				}
				projectionStates, stateErr := qtx.ListMetadataProjectionStatesByEntities(ctx, entityIDs)
				if stateErr != nil {
					return fmt.Errorf("read metadata projection checkpoints: %w", stateErr)
				}
				type checkpointKey struct {
					localKind string
					localID   int64
					scope     string
				}
				checkpoints := make(map[checkpointKey]sqlc.MetadataProjectionState, len(projectionStates))
				for _, projectionState := range projectionStates {
					checkpoints[checkpointKey{projectionState.LocalKind, projectionState.LocalID, projectionState.Scope}] = projectionState
				}
				for _, target := range scopeTargets {
					changeState := changesByEntity[target.EntityID]
					if _, changed := changeState.scopes[metadatasync.ArtistTopTracksScope]; !changed || target.LocalKind != "artist" || target.EntityKind != "artist" {
						continue
					}
					checkpoint, exists := checkpoints[checkpointKey{target.LocalKind, target.LocalID, metadatasync.ArtistTopTracksScope}]
					if exists && checkpoint.EntityID == target.EntityID && changeState.change.ProjectionVersion > 0 && checkpoint.ProjectionVersion >= changeState.change.ProjectionVersion {
						continue
					}
					args := ReconcileMetadataScopeArgs{
						LocalKind: target.LocalKind, LocalID: target.LocalID,
						EntityID: target.EntityID.String(), EntityKind: target.EntityKind,
						Scope:             metadatasync.ArtistTopTracksScope,
						ProjectionVersion: changeState.change.ProjectionVersion,
					}
					opts := args.InsertOpts()
					jobs = append(jobs, river.InsertManyParams{Args: args, InsertOpts: &opts})
				}
			}
			// Replace each item's queued metadata-change refresh with this newer
			// trailing refresh. A running refresh is intentionally preserved; the
			// replacement runs after it and observes the newest upstream state.
			if _, cancelErr := queueops.CancelPendingMetadataChangeEnrichJobs(ctx, tx, mediaRefreshIDs); cancelErr != nil {
				return fmt.Errorf("replace pending metadata change enrich jobs: %w", cancelErr)
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
				scopeQueued, scopeBackfillErr := enqueueMetadataScopeBackfill(ctx, tx, rc, 25)
				if scopeBackfillErr != nil {
					return scopeBackfillErr
				}
				enqueued += scopeQueued
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

// enqueueMetadataScopeBackfill repairs bindings created before per-scope
// checkpoints existed, plus any projection that lagged a later parent refresh.
// Successful empty projections have a checkpoint and naturally fall out of
// this query. Active jobs and recently discarded River jobs suppress churn.
func enqueueMetadataScopeBackfill(ctx context.Context, tx pgx.Tx, rc *river.Client[pgx.Tx], limit int) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT binding.local_kind, binding.local_id, binding.entity_id,
		       binding.entity_kind, binding.projection_version
		FROM metadata_entity_bindings binding
		JOIN artists artist ON artist.id = binding.local_id
		LEFT JOIN metadata_projection_states state
		  ON state.local_kind = binding.local_kind
		 AND state.local_id = binding.local_id
		 AND state.scope = 'top_tracks'
		WHERE binding.local_kind = 'artist'
		  AND binding.entity_kind = 'artist'
		  AND (
		    state.local_id IS NULL
		    OR state.entity_id <> binding.entity_id
		    OR state.projection_version < binding.projection_version
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.kind = 'reconcile_metadata_scope'
		      AND job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled', 'discarded')
		      AND job.args->>'local_kind' = binding.local_kind
		      AND NULLIF(job.args->>'local_id', '')::bigint = binding.local_id
		      AND job.args->>'scope' = 'top_tracks'
		  )
		ORDER BY state.applied_at NULLS FIRST, binding.local_id
		LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("select metadata scope backfill: %w", err)
	}
	defer rows.Close()

	jobs := make([]river.InsertManyParams, 0, limit)
	for rows.Next() {
		var args ReconcileMetadataScopeArgs
		var entityID uuid.UUID
		if err := rows.Scan(&args.LocalKind, &args.LocalID, &entityID, &args.EntityKind, &args.ProjectionVersion); err != nil {
			return 0, fmt.Errorf("scan metadata scope backfill: %w", err)
		}
		args.EntityID = entityID.String()
		args.Scope = metadatasync.ArtistTopTracksScope
		opts := args.InsertOpts()
		opts.Priority = PriorityAnalysis
		jobs = append(jobs, river.InsertManyParams{Args: args, InsertOpts: &opts})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read metadata scope backfill: %w", err)
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	results, err := rc.InsertManyTx(ctx, tx, jobs)
	if err != nil {
		return 0, fmt.Errorf("enqueue metadata scope backfill: %w", err)
	}
	queued := 0
	for _, result := range results {
		if !result.UniqueSkippedAsDuplicate {
			queued++
		}
	}
	return queued, nil
}
