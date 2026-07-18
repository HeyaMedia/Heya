package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/metadatasync"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

type metadataTopTracksSource interface {
	ArtistTopTrackEntries(context.Context, string, ...heyametadata.ProviderCredentials) ([]metadata.TopTrackEntry, error)
}

// ReconcileMetadataScopeWorker is the retryable bridge between canonical
// changed_scopes and local child projections. The dispatcher is intentionally
// generic; top_tracks is the first registered projection, and other media
// scopes can join without adding another feed or cursor.
type ReconcileMetadataScopeWorker struct {
	river.WorkerDefaults[ReconcileMetadataScopeArgs]
	DB     *pgxpool.Pool
	Source metadataTopTracksSource
}

func (w *ReconcileMetadataScopeWorker) Work(ctx context.Context, job *river.Job[ReconcileMetadataScopeArgs]) error {
	args := job.Args
	if w.DB == nil {
		return fmt.Errorf("reconcile metadata scope: database is required")
	}
	if args.LocalKind != "artist" || args.EntityKind != "artist" || args.Scope != metadatasync.ArtistTopTracksScope {
		return fmt.Errorf("reconcile metadata scope: unsupported target %s/%s scope %q", args.LocalKind, args.EntityKind, args.Scope)
	}
	if w.Source == nil {
		return fmt.Errorf("reconcile metadata scope: metadata client is required")
	}
	entityID, err := uuid.Parse(args.EntityID)
	if err != nil {
		return fmt.Errorf("reconcile metadata scope: invalid entity ID %q: %w", args.EntityID, err)
	}

	// Avoid an HTTP request for stale/replayed jobs. The binding is checked
	// again under a row lock before writing, so re-identification is safe.
	q := sqlc.New(w.DB)
	binding, err := q.GetMetadataEntityBinding(ctx, sqlc.GetMetadataEntityBindingParams{LocalKind: args.LocalKind, LocalID: args.LocalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reconcile metadata scope: read binding: %w", err)
	}
	if binding.EntityID != entityID || binding.EntityKind != args.EntityKind {
		return nil
	}
	if _, err := q.GetArtistByID(ctx, args.LocalID); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("reconcile metadata scope: read local artist: %w", err)
	}
	desiredVersion := max(args.ProjectionVersion, binding.ProjectionVersion)
	if state, stateErr := q.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{
		LocalKind: args.LocalKind, LocalID: args.LocalID, Scope: args.Scope,
	}); stateErr == nil && args.ProjectionVersion > 0 && state.EntityID == entityID && state.ProjectionVersion >= desiredVersion {
		return nil
	} else if stateErr != nil && !errors.Is(stateErr, pgx.ErrNoRows) {
		return fmt.Errorf("reconcile metadata scope: read checkpoint: %w", stateErr)
	}

	tracks, err := w.Source.ArtistTopTrackEntries(ctx, args.EntityID)
	if err != nil {
		return fmt.Errorf("reconcile metadata scope: fetch artist top tracks: %w", err)
	}

	tx, err := w.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("reconcile metadata scope: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := sqlc.New(tx)
	lockedBinding, err := qtx.GetMetadataEntityBindingForUpdate(ctx, sqlc.GetMetadataEntityBindingForUpdateParams{
		LocalKind: args.LocalKind, LocalID: args.LocalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reconcile metadata scope: lock binding: %w", err)
	}
	if lockedBinding.EntityID != entityID || lockedBinding.EntityKind != args.EntityKind {
		return nil
	}
	desiredVersion = max(args.ProjectionVersion, lockedBinding.ProjectionVersion)
	if state, stateErr := qtx.GetMetadataProjectionState(ctx, sqlc.GetMetadataProjectionStateParams{
		LocalKind: args.LocalKind, LocalID: args.LocalID, Scope: args.Scope,
	}); stateErr == nil && args.ProjectionVersion > 0 && state.EntityID == entityID && state.ProjectionVersion >= desiredVersion {
		return nil
	} else if stateErr != nil && !errors.Is(stateErr, pgx.ErrNoRows) {
		return fmt.Errorf("reconcile metadata scope: recheck checkpoint: %w", stateErr)
	}
	if err := metadatasync.ReplaceArtistTopTracks(ctx, qtx, args.LocalID, entityID, args.EntityKind, desiredVersion, tracks); err != nil {
		return fmt.Errorf("reconcile metadata scope: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconcile metadata scope: commit: %w", err)
	}
	log.Info().Int64("local_id", args.LocalID).Str("entity_id", args.EntityID).
		Str("scope", args.Scope).Int("rows", len(tracks)).Int64("projection_version", desiredVersion).
		Msg("canonical metadata scope reconciled")
	return nil
}
