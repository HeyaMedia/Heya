package worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// GeneratedWriteSuppressor lets sidecar workers hand exact output evidence to
// the watcher/provenance owner. Implementations durably acknowledge every
// attestation and suppress live events only for publications made this call;
// later user changes to the same path must remain visible.
type GeneratedWriteSuppressor interface {
	SuppressGeneratedWrite(generatedwrite.Output) error
}

type generatedWriteValidator func(context.Context) (bool, error)

type generatedWriteSetting int

const (
	generatedWriteNFO generatedWriteSetting = iota
	generatedWriteImage
)

// generatedWriteAllowed is the execution-time circuit breaker for queued
// sidecar jobs. Enqueue-time settings are only a snapshot: a large scanner
// fanout can leave thousands of jobs waiting after an administrator disables
// NFO or image exports. Every worker must consult the current library setting
// immediately before publishing so those old jobs cannot recreate sidecars.
func generatedWriteAllowed(ctx context.Context, q *sqlc.Queries, libraryID int64, setting generatedWriteSetting) (bool, error) {
	library, err := q.GetLibraryByID(ctx, libraryID)
	if err != nil {
		return false, err
	}
	settings := metadata.ParseSettings(library.Settings)
	switch setting {
	case generatedWriteNFO:
		return settings.SaveNFO, nil
	case generatedWriteImage:
		return settings.SaveImages, nil
	default:
		return false, fmt.Errorf("unknown generated-write setting %d", setting)
	}
}

func publishGeneratedWrite(ctx context.Context, db *pgxpool.Pool, suppressor GeneratedWriteSuppressor, prepared *generatedwrite.Prepared) error {
	if prepared == nil {
		return nil
	}
	_, _, err := generatedwrite.Publish(ctx, db, suppressor, prepared)
	if err != nil {
		log.Warn().Err(vfs.RedactError(err)).Str("path", vfs.RedactPath(prepared.Path())).Msg("could not publish generated sidecar")
		return fmt.Errorf("publish generated sidecar: %w", err)
	}
	return nil
}

// publishGeneratedWriteWhenAllowed closes the meaningful settings race around
// staging. PrepareFile may copy a large image over NFS; if exports are disabled
// while that happens, discard the staged temp file instead of publishing it.
func publishGeneratedWriteWhenAllowed(
	ctx context.Context,
	db *pgxpool.Pool,
	suppressor GeneratedWriteSuppressor,
	q *sqlc.Queries,
	libraryID int64,
	setting generatedWriteSetting,
	prepared *generatedwrite.Prepared,
	validate generatedWriteValidator,
) error {
	if prepared == nil {
		return nil
	}
	allowed, err := generatedWriteAllowed(ctx, q, libraryID, setting)
	if err != nil {
		return errors.Join(err, prepared.Discard())
	}
	if !allowed {
		return prepared.Discard()
	}
	if validate != nil {
		valid, validationErr := validate(ctx)
		if validationErr != nil {
			return errors.Join(validationErr, prepared.Discard())
		}
		if !valid {
			return prepared.Discard()
		}
	}
	return publishGeneratedWrite(ctx, db, suppressor, prepared)
}
